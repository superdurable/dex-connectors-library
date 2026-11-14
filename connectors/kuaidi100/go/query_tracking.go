// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kuaidi100

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// QueryTrackingInput identifies one carrier waybill. Phone is only needed by carriers requiring recipient verification.
type QueryTrackingInput struct {
	// CarrierCode is the lowercase code from Kuaidi100's official carrier-code table.
	CarrierCode string `json:"carrierCode"`
	// TrackingNumber is the 6–32 character carrier waybill number.
	TrackingNumber string `json:"trackingNumber"`
	// Phone is the sender or recipient phone for carriers such as SF Express and ZTO; it is never returned.
	Phone string `json:"phone,omitempty"`
}

// TrackingEvent is one provider-reported event, in latest-first order.
type TrackingEvent struct {
	// Time is the provider's event timestamp string.
	Time string `json:"time"`
	// Content is the provider's parcel-status narrative.
	Content string `json:"content"`
}

// QueryTrackingOutput contains one waybill's current state and latest-first events.
type QueryTrackingOutput struct {
	// CarrierCode is the provider-confirmed carrier code.
	CarrierCode string `json:"carrierCode"`
	// TrackingNumber is the provider-confirmed waybill number.
	TrackingNumber string `json:"trackingNumber"`
	// State is the provider's open state code; unknown future codes are preserved.
	State string `json:"state"`
	// Events contains the carrier events in provider order, newest first.
	Events []TrackingEvent `json:"events"`
}

// QueryTrackingOperation reads a single parcel's current tracking history.
type QueryTrackingOperation struct{ client *Client }

// Definition returns the generated Query contract.
func (QueryTrackingOperation) Definition() sdkgo.QueryDefinition { return QueryTrackingDefinition }

var carrierCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
var trackingNumberPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{6,32}$`)
var phonePattern = regexp.MustCompile(`^[0-9+*#-]{4,32}$`)

// Invoke signs the exact JSON parameter bytes and classifies provider outcomes without disclosing credentials.
func (operation QueryTrackingOperation) Invoke(call sdkgo.Call, input QueryTrackingInput) sdkgo.QueryAttempt[QueryTrackingOutput] {
	if !carrierCodePattern.MatchString(input.CarrierCode) || !trackingNumberPattern.MatchString(input.TrackingNumber) || (input.Phone != "" && !phonePattern.MatchString(input.Phone)) {
		return trackingBranch(QueryTrackingBranchDefect, sdkgo.FailureValidation, "invalid carrier code, tracking number, or phone")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidTrackingCredential(credentials.Customer.Reveal()) || !isValidTrackingCredential(credentials.Key.Reveal()) {
		return trackingBranch(QueryTrackingBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	param := struct {
		Com   string `json:"com"`
		Num   string `json:"num"`
		Phone string `json:"phone,omitempty"`
		Show  string `json:"show"`
		Order string `json:"order"`
	}{input.CarrierCode, input.TrackingNumber, input.Phone, "0", "desc"}
	paramJSON, err := json.Marshal(param)
	if err != nil {
		return trackingBranch(QueryTrackingBranchDefect, sdkgo.FailureValidation, "tracking parameters are invalid")
	}
	digest := md5.Sum([]byte(string(paramJSON) + credentials.Key.Reveal() + credentials.Customer.Reveal()))
	form := url.Values{"customer": {credentials.Customer.Reveal()}, "sign": {strings.ToUpper(hex.EncodeToString(digest[:]))}, "param": {string(paramJSON)}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/poll/query.do", strings.NewReader(form.Encode()))
	if err != nil {
		return trackingBranch(QueryTrackingBranchDefect, sdkgo.FailureValidation, "tracking request is invalid")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return trackingRetry(sdkgo.FailureAvailability, "Kuaidi100 request failed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests {
		return trackingRetry(sdkgo.FailureRateLimit, "Kuaidi100 rate limited the request")
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return trackingRetry(sdkgo.FailureAvailability, "Kuaidi100 is temporarily unavailable")
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return trackingBranch(QueryTrackingBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Kuaidi100 response exceeded the configured limit")
	}
	if err != nil {
		return trackingRetry(sdkgo.FailureAvailability, "Kuaidi100 response was interrupted")
	}
	var wire struct {
		Result     *bool  `json:"result"`
		ReturnCode string `json:"returnCode"`
		State      string `json:"state"`
		Status     string `json:"status"`
		Com        string `json:"com"`
		Nu         string `json:"nu"`
		Data       []struct {
			Time    string `json:"time"`
			Context string `json:"context"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil {
		return trackingBranch(QueryTrackingBranchInvalidResponse, sdkgo.FailureProtocol, "Kuaidi100 returned invalid JSON")
	}
	if wire.Result != nil && !*wire.Result {
		switch wire.ReturnCode {
		case "501", "502":
			return trackingRetry(sdkgo.FailureAvailability, "Kuaidi100 is temporarily unavailable")
		case "504":
			return trackingBranch(QueryTrackingBranchProviderRejected, sdkgo.FailureRateLimit, "Kuaidi100 locked this waybill after excessive queries")
		case "503", "601":
			return trackingBranch(QueryTrackingBranchProviderRejected, sdkgo.FailureAuthentication, "Kuaidi100 rejected the account credentials")
		default:
			return trackingBranch(QueryTrackingBranchProviderRejected, sdkgo.FailureProviderRejection, "Kuaidi100 rejected the tracking query")
		}
	}
	if response.StatusCode != http.StatusOK {
		return trackingBranch(QueryTrackingBranchProviderRejected, sdkgo.FailureProviderRejection, "Kuaidi100 rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if wire.Status != "200" || wire.Com != input.CarrierCode || wire.Nu != input.TrackingNumber || wire.State == "" || wire.Data == nil {
		return trackingBranch(QueryTrackingBranchInvalidResponse, sdkgo.FailureProtocol, "Kuaidi100 returned incomplete or mismatched tracking data")
	}
	events := make([]TrackingEvent, 0, len(wire.Data))
	for _, item := range wire.Data {
		if item.Time == "" || item.Context == "" {
			return trackingBranch(QueryTrackingBranchInvalidResponse, sdkgo.FailureProtocol, "Kuaidi100 returned an incomplete tracking event")
		}
		events = append(events, TrackingEvent{Time: item.Time, Content: item.Context})
	}
	return sdkgo.NewQueryBranch(QueryTrackingBranchRetrieved, QueryTrackingOutput{CarrierCode: wire.Com, TrackingNumber: wire.Nu, State: wire.State, Events: events}, nil, sdkgo.Receipt{})
}

func isValidTrackingCredential(value string) bool {
	return value != "" && len(value) <= 512 && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\t ")
}
func trackingBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[QueryTrackingOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "kuaidi100", Operation: "queryTracking", Message: message}
	return sdkgo.NewQueryBranch(branch, QueryTrackingOutput{}, &failure, sdkgo.Receipt{})
}
func trackingRetry(kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[QueryTrackingOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "kuaidi100", Operation: "queryTracking", Message: message}
	return sdkgo.NewQueryRetry[QueryTrackingOutput](failure, 30*time.Minute)
}
