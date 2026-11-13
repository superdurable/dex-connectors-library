// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sfexpress

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// QueryRoutesInput identifies one SF Express waybill owned by the application.
type QueryRoutesInput struct {
	// WaybillNumber is an SF Express waybill number, not a customer order number.
	WaybillNumber string `json:"waybillNumber"`
}

// RouteEvent contains the time and code of a delivery event.
type RouteEvent struct {
	// OccurredAt is the provider's local timestamp (YYYY-MM-DD HH:mm:ss).
	OccurredAt string `json:"occurredAt"`
	// Opcode is the provider's event code.
	Opcode string `json:"opcode"`
}

// RouteQueryResult contains a waybill and its bounded standard route history.
type RouteQueryResult struct {
	// WaybillNumber is the SF Express waybill number.
	WaybillNumber string `json:"waybillNumber"`
	// Routes contains at most 100 route events, without locations or contacts.
	Routes []RouteEvent `json:"routes"`
}

// QueryRoutesOperation reads standard route history for one owned waybill.
type QueryRoutesOperation struct{ client *Client }

// Definition returns the generated route Query contract.
func (QueryRoutesOperation) Definition() sdkgo.QueryDefinition { return QueryRoutesDefinition }

// Invoke signs and sends one route query, then classifies transport and provider outcomes.
func (operation QueryRoutesOperation) Invoke(call sdkgo.Call, input QueryRoutesInput) sdkgo.QueryAttempt[RouteQueryResult] {
	if !validWaybill(input.WaybillNumber) {
		return routeBranch(QueryRoutesBranchDefect, sdkgo.FailureValidation, "SF Express waybill number is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validPartnerID(credentials.PartnerID) || !validCheckword(credentials.Checkword.Reveal()) {
		return routeBranch(QueryRoutesBranchDefect, sdkgo.FailureAuthentication, "SF Express credentials are unavailable or invalid")
	}
	message, err := json.Marshal(struct {
		Language       string   `json:"language"`
		TrackingType   string   `json:"trackingType"`
		TrackingNumber []string `json:"trackingNumber"`
		MethodType     string   `json:"methodType"`
	}{"0", "1", []string{input.WaybillNumber}, "1"})
	if err != nil {
		return routeBranch(QueryRoutesBranchDefect, sdkgo.FailureValidation, "SF Express route request is invalid")
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	requestID, err := newRequestID()
	if err != nil {
		return routeRetry(sdkgo.FailureAvailability, "SF Express request ID generation failed", 0)
	}
	form := url.Values{
		"partnerID":   {credentials.PartnerID},
		"requestID":   {requestID},
		"serviceCode": {"EXP_RECE_SEARCH_ROUTES"},
		"timestamp":   {timestamp},
		"msgData":     {string(message)},
		"msgDigest":   {signRouteMessage(string(message), timestamp, credentials.Checkword.Reveal())},
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/std/service", strings.NewReader(form.Encode()))
	if err != nil {
		return routeBranch(QueryRoutesBranchDefect, sdkgo.FailureValidation, "SF Express route request is invalid")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return routeRetry(sdkgo.FailureAvailability, "SF Express route request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return routeRetry(sdkgo.FailureRateLimit, "SF Express rate limited route query", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return routeRetry(sdkgo.FailureAvailability, "SF Express route API is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return routeBranch(QueryRoutesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "SF Express route response exceeded the configured limit")
	}
	if err != nil {
		return routeRetry(sdkgo.FailureAvailability, "SF Express route response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return routeBranch(QueryRoutesBranchProviderRejected, classifyRouteHTTP(response.StatusCode), "SF Express rejected route query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	result, isRejected, isRetry, err := decodeRouteResponse(body, input.WaybillNumber)
	if err != nil {
		return routeBranch(QueryRoutesBranchInvalidResponse, sdkgo.FailureProtocol, "SF Express returned an invalid route response")
	}
	if isRetry {
		return routeRetry(sdkgo.FailureAvailability, "SF Express route service is temporarily unavailable", retryAfter)
	}
	if isRejected {
		return routeBranch(QueryRoutesBranchProviderRejected, sdkgo.FailureProviderRejection, "SF Express rejected route query")
	}
	return sdkgo.NewQueryBranch(QueryRoutesBranchFound, result, nil, sdkgo.Receipt{})
}

func decodeRouteResponse(body []byte, expectedWaybill string) (RouteQueryResult, bool, bool, error) {
	var envelope struct {
		Code string          `json:"apiResultCode"`
		Data json.RawMessage `json:"apiResultData"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Code == "" {
		return RouteQueryResult{}, false, false, errors.New("invalid SF Express envelope")
	}
	if envelope.Code == "A1005" || envelope.Code == "A1009" || envelope.Code == "A1099" {
		return RouteQueryResult{}, false, true, nil
	}
	if envelope.Code != "A1000" {
		return RouteQueryResult{}, true, false, nil
	}
	var encoded string
	if err := json.Unmarshal(envelope.Data, &encoded); err != nil {
		return RouteQueryResult{}, false, false, errors.New("invalid SF Express business payload")
	}
	var business struct {
		Success   *bool  `json:"success"`
		ErrorCode string `json:"errorCode"`
		Data      *struct {
			RouteResponses []struct {
				MailNo string `json:"mailNo"`
				Routes []struct {
					AcceptTime string `json:"acceptTime"`
					Remark     string `json:"remark"`
					Opcode     string `json:"opcode"`
				} `json:"routes"`
			} `json:"routeResps"`
		} `json:"msgData"`
	}
	if err := json.Unmarshal([]byte(encoded), &business); err != nil || business.Success == nil {
		return RouteQueryResult{}, false, false, errors.New("invalid SF Express business response")
	}
	if !*business.Success {
		if business.ErrorCode == "A1005" || business.ErrorCode == "A1009" || business.ErrorCode == "A1099" {
			return RouteQueryResult{}, false, true, nil
		}
		return RouteQueryResult{}, true, false, nil
	}
	if business.Data == nil || len(business.Data.RouteResponses) != 1 {
		return RouteQueryResult{}, false, false, errors.New("missing SF Express route list")
	}
	routeResponse := business.Data.RouteResponses[0]
	if routeResponse.MailNo != expectedWaybill || routeResponse.Routes == nil || len(routeResponse.Routes) > 100 {
		return RouteQueryResult{}, false, false, errors.New("invalid SF Express route list")
	}
	result := RouteQueryResult{WaybillNumber: routeResponse.MailNo, Routes: make([]RouteEvent, 0, len(routeResponse.Routes))}
	for _, route := range routeResponse.Routes {
		if route.AcceptTime == "" || route.Remark == "" || route.Opcode == "" {
			return RouteQueryResult{}, false, false, errors.New("invalid SF Express route event")
		}
		result.Routes = append(result.Routes, RouteEvent{OccurredAt: route.AcceptTime, Opcode: route.Opcode})
	}
	return result, false, false, nil
}

func signRouteMessage(message, timestamp, checkword string) string {
	// SF Express's official SDK URL-encodes the concatenation before MD5 and Base64.
	encoded := url.QueryEscape(message + timestamp + checkword)
	digest := md5.Sum([]byte(encoded))
	return base64.StdEncoding.EncodeToString(digest[:])
}

func newRequestID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	raw := hex.EncodeToString(bytes[:])
	return raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:], nil
}

func validWaybill(value string) bool {
	if len(value) < 8 || len(value) > 30 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			if character < 'A' || character > 'Z' {
				return false
			}
		}
	}
	return true
}
func validPartnerID(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func validCheckword(value string) bool {
	return value != "" && len(value) <= 2048 && strings.TrimSpace(value) == value
}
func classifyRouteHTTP(status int) sdkgo.FailureKind {
	switch status {
	case 401:
		return sdkgo.FailureAuthentication
	case 403:
		return sdkgo.FailureAuthorization
	case 400, 404, 412:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func routeBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[RouteQueryResult] {
	failure := sdkgo.Failure{Kind: kind, Provider: "sfexpress", Operation: "queryRoutes", Message: message}
	return sdkgo.NewQueryBranch(branch, RouteQueryResult{}, &failure, sdkgo.Receipt{})
}
func routeRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[RouteQueryResult] {
	failure := sdkgo.Failure{Kind: kind, Provider: "sfexpress", Operation: "queryRoutes", Message: message}
	return sdkgo.NewQueryRetry[RouteQueryResult](failure, delay)
}
