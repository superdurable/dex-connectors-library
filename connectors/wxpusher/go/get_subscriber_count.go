// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wxpusher

import (
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

// GetSubscriberCountInput has no user-supplied parameters.
type GetSubscriberCountInput struct{}

// SubscriberCountResult contains the provider's total number of subscription records.
type SubscriberCountResult struct {
	// Total is the number of subscription records; one user may create multiple records.
	Total int64 `json:"total"`
}

// GetSubscriberCountOperation reads the authorized application's subscription-record count.
type GetSubscriberCountOperation struct{ client *Client }

// Definition returns the generated subscriber-count Query contract.
func (GetSubscriberCountOperation) Definition() sdkgo.QueryDefinition {
	return GetSubscriberCountDefinition
}

// Invoke fetches only one user record and discards it after reading the aggregate total.
func (operation GetSubscriberCountOperation) Invoke(call sdkgo.Call, _ GetSubscriberCountInput) sdkgo.QueryAttempt[SubscriberCountResult] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validAppToken(credentials.AppToken.Reveal()) {
		return countBranch(GetSubscriberCountBranchDefect, sdkgo.FailureAuthentication, "WxPusher application token is unavailable or invalid")
	}
	query := url.Values{"appToken": {credentials.AppToken.Reveal()}, "page": {"1"}, "pageSize": {"1"}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/api/fun/wxuser/v2?"+query.Encode(), nil)
	if err != nil {
		return countBranch(GetSubscriberCountBranchDefect, sdkgo.FailureValidation, "WxPusher subscriber request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return countRetry(sdkgo.FailureAvailability, "WxPusher subscriber request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return countRetry(sdkgo.FailureRateLimit, "WxPusher rate limited subscriber query", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return countRetry(sdkgo.FailureAvailability, "WxPusher subscriber API is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return countBranch(GetSubscriberCountBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "WxPusher response exceeded the configured limit")
	}
	if err != nil {
		return countRetry(sdkgo.FailureAvailability, "WxPusher subscriber response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return countBranch(GetSubscriberCountBranchProviderRejected, classifyCountHTTP(response.StatusCode), "WxPusher rejected subscriber query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	result, isRejected, err := decodeSubscriberCount(body)
	if err != nil {
		return countBranch(GetSubscriberCountBranchInvalidResponse, sdkgo.FailureProtocol, "WxPusher returned an invalid subscriber response")
	}
	if isRejected {
		return countBranch(GetSubscriberCountBranchProviderRejected, sdkgo.FailureProviderRejection, "WxPusher rejected subscriber query")
	}
	return sdkgo.NewQueryBranch(GetSubscriberCountBranchCounted, result, nil, sdkgo.Receipt{})
}
func decodeSubscriberCount(body []byte) (SubscriberCountResult, bool, error) {
	var envelope struct {
		Code    *int  `json:"code"`
		Success *bool `json:"success"`
		Data    *struct {
			Total    *int64          `json:"total"`
			Page     *int            `json:"page"`
			PageSize *int            `json:"pageSize"`
			Records  json.RawMessage `json:"records"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Code == nil {
		return SubscriberCountResult{}, false, errors.New("invalid WxPusher response")
	}
	if *envelope.Code != 1000 {
		return SubscriberCountResult{}, true, nil
	}
	if envelope.Success == nil || !*envelope.Success || envelope.Data == nil || envelope.Data.Total == nil || *envelope.Data.Total < 0 || envelope.Data.Page == nil || *envelope.Data.Page != 1 || envelope.Data.PageSize == nil || *envelope.Data.PageSize != 1 || len(envelope.Data.Records) == 0 {
		return SubscriberCountResult{}, false, errors.New("invalid WxPusher subscriber count")
	}
	return SubscriberCountResult{Total: *envelope.Data.Total}, false, nil
}
func validAppToken(value string) bool {
	if !strings.HasPrefix(value, "AT_") || len(value) > 256 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func classifyCountHTTP(status int) sdkgo.FailureKind {
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
func countBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[SubscriberCountResult] {
	failure := sdkgo.Failure{Kind: kind, Provider: "wxpusher", Operation: "getSubscriberCount", Message: message}
	return sdkgo.NewQueryBranch(branch, SubscriberCountResult{}, &failure, sdkgo.Receipt{})
}
func countRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[SubscriberCountResult] {
	failure := sdkgo.Failure{Kind: kind, Provider: "wxpusher", Operation: "getSubscriberCount", Message: message}
	return sdkgo.NewQueryRetry[SubscriberCountResult](failure, delay)
}
