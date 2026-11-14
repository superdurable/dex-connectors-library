// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kuaidibird

import (
	"crypto/md5"
	"encoding/base64"
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

// QueryTrackingInput identifies one carrier waybill for the base instant-query API.
type QueryTrackingInput struct {
	// CarrierCode is the uppercase code from KuaidiBird's official carrier table.
	CarrierCode string `json:"carrierCode"`
	// TrackingNumber is the carrier waybill number, at most 30 characters.
	TrackingNumber string `json:"trackingNumber"`
}

// TrackingEvent is one provider-reported event in the provider's returned order.
type TrackingEvent struct {
	// Time is the event timestamp string returned by KuaidiBird.
	Time string `json:"time"`
	// Content is the carrier's event description; carriers may include contact details in this text.
	Content string `json:"content"`
}

// QueryTrackingOutput contains the current state and event history for one waybill.
type QueryTrackingOutput struct {
	// CarrierCode is the provider-confirmed carrier code.
	CarrierCode string `json:"carrierCode"`
	// TrackingNumber is the provider-confirmed waybill number.
	TrackingNumber string `json:"trackingNumber"`
	// State is the provider's open state code, preserving unknown future codes.
	State string `json:"state"`
	// Events preserves the provider's event order.
	Events []TrackingEvent `json:"events"`
}

// QueryTrackingOperation reads one waybill through KuaidiBird's instant-query service.
type QueryTrackingOperation struct{ client *Client }

// Definition returns the generated Query contract.
func (QueryTrackingOperation) Definition() sdkgo.QueryDefinition { return QueryTrackingDefinition }

var carrierCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,19}$`)
var trackingNumberPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{6,30}$`)
var businessIDPattern = regexp.MustCompile(`^[0-9]{1,20}$`)

// Invoke signs the exact request JSON and classifies provider outcomes without revealing credentials.
func (operation QueryTrackingOperation) Invoke(call sdkgo.Call, input QueryTrackingInput) sdkgo.QueryAttempt[QueryTrackingOutput] {
	if !carrierCodePattern.MatchString(input.CarrierCode) || !trackingNumberPattern.MatchString(input.TrackingNumber) {
		return trackingBranch(QueryTrackingBranchDefect, sdkgo.FailureValidation, "invalid carrier code or tracking number")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !businessIDPattern.MatchString(credentials.BusinessID.Reveal()) || !isValidTrackingCredential(credentials.APIKey.Reveal()) {
		return trackingBranch(QueryTrackingBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	requestData, err := json.Marshal(struct {
		ShipperCode  string `json:"ShipperCode"`
		LogisticCode string `json:"LogisticCode"`
	}{input.CarrierCode, input.TrackingNumber})
	if err != nil {
		return trackingBranch(QueryTrackingBranchDefect, sdkgo.FailureValidation, "tracking parameters are invalid")
	}
	dataSign := signTrackingRequest(string(requestData), credentials.APIKey.Reveal())
	form := url.Values{"RequestData": {string(requestData)}, "EBusinessID": {credentials.BusinessID.Reveal()}, "RequestType": {"1002"}, "DataSign": {dataSign}, "DataType": {"2"}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/Ebusiness/EbusinessOrderHandle.aspx", strings.NewReader(form.Encode()))
	if err != nil {
		return trackingBranch(QueryTrackingBranchDefect, sdkgo.FailureValidation, "tracking request is invalid")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return trackingRetry(sdkgo.FailureAvailability, "KuaidiBird request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return trackingRetry(sdkgo.FailureRateLimit, "KuaidiBird rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return trackingRetry(sdkgo.FailureAvailability, "KuaidiBird is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return trackingBranch(QueryTrackingBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "KuaidiBird response exceeded the configured limit")
	}
	if err != nil {
		return trackingRetry(sdkgo.FailureAvailability, "KuaidiBird response was interrupted", 0)
	}
	var wire struct {
		BusinessID     string          `json:"EBusinessID"`
		CarrierCode    string          `json:"ShipperCode"`
		TrackingNumber string          `json:"LogisticCode"`
		Success        *bool           `json:"Success"`
		State          json.RawMessage `json:"State"`
		Traces         []struct {
			Time    string `json:"AcceptTime"`
			Content string `json:"AcceptStation"`
		} `json:"Traces"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Success == nil {
		return trackingBranch(QueryTrackingBranchInvalidResponse, sdkgo.FailureProtocol, "KuaidiBird returned an invalid response")
	}
	if response.StatusCode != http.StatusOK {
		return trackingBranch(QueryTrackingBranchProviderRejected, sdkgo.FailureProviderRejection, "KuaidiBird rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if !*wire.Success {
		return trackingBranch(QueryTrackingBranchProviderRejected, sdkgo.FailureProviderRejection, "KuaidiBird could not return this waybill")
	}
	state, err := decodeTrackingState(wire.State)
	if err != nil || wire.BusinessID != credentials.BusinessID.Reveal() || wire.CarrierCode != input.CarrierCode || wire.TrackingNumber != input.TrackingNumber || wire.Traces == nil {
		return trackingBranch(QueryTrackingBranchInvalidResponse, sdkgo.FailureProtocol, "KuaidiBird returned incomplete or mismatched tracking data")
	}
	events := make([]TrackingEvent, 0, len(wire.Traces))
	for _, trace := range wire.Traces {
		if trace.Time == "" || trace.Content == "" {
			return trackingBranch(QueryTrackingBranchInvalidResponse, sdkgo.FailureProtocol, "KuaidiBird returned an incomplete tracking event")
		}
		events = append(events, TrackingEvent{Time: trace.Time, Content: trace.Content})
	}
	return sdkgo.NewQueryBranch(QueryTrackingBranchRetrieved, QueryTrackingOutput{CarrierCode: wire.CarrierCode, TrackingNumber: wire.TrackingNumber, State: state, Events: events}, nil, sdkgo.Receipt{})
}

func signTrackingRequest(requestData, apiKey string) string {
	digest := md5.Sum([]byte(requestData + apiKey))
	hexDigest := hex.EncodeToString(digest[:])
	return base64.StdEncoding.EncodeToString([]byte(hexDigest))
}
func decodeTrackingState(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", errors.New("missing state")
	}
	var state string
	if json.Unmarshal(raw, &state) == nil {
		if state == "" {
			return "", errors.New("empty state")
		}
		return state, nil
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String(), nil
	}
	return "", errors.New("invalid state")
}
func isValidTrackingCredential(value string) bool {
	return value != "" && len(value) <= 512 && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\t ")
}
func trackingBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[QueryTrackingOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "kuaidibird", Operation: "queryTracking", Message: message}
	return sdkgo.NewQueryBranch(branch, QueryTrackingOutput{}, &failure, sdkgo.Receipt{})
}
func trackingRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[QueryTrackingOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "kuaidibird", Operation: "queryTracking", Message: message}
	return sdkgo.NewQueryRetry[QueryTrackingOutput](failure, delay)
}
