// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package istero

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

const accountEndpoint = "https://api.istero.com/resource/v1/isas/mine/information"

// GetAccountStatusInput requests the account state associated with the configured Token and ISID.
type GetAccountStatusInput struct{}

// GetAccountStatusOutput contains only the account ID and daily call counts.
type GetAccountStatusOutput struct {
	// ISID is the numeric ISAS account ID.
	ISID int64 `json:"isid"`
	// TodayUsed is the number of calls made today.
	TodayUsed int64 `json:"todayUsed"`
	// TodayRemaining is the remaining daily call allowance.
	TodayRemaining int64 `json:"todayRemaining"`
}

// GetAccountStatusOperation reads the free ISAS account endpoint.
type GetAccountStatusOperation struct{ client *Client }

// Definition returns the generated account-status contract.
func (GetAccountStatusOperation) Definition() sdkgo.QueryDefinition {
	return GetAccountStatusDefinition
}

// Invoke gets account status and classifies provider and transport outcomes.
func (operation GetAccountStatusOperation) Invoke(call sdkgo.Call, _ GetAccountStatusInput) sdkgo.QueryAttempt[GetAccountStatusOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) || !isValidISID(credentials.ISID) {
		return accountStatusBranch(GetAccountStatusBranchDefect, sdkgo.FailureAuthentication, "ISAS credentials are unavailable or invalid")
	}
	requestURL, err := url.Parse(accountEndpoint)
	if err != nil {
		return accountStatusBranch(GetAccountStatusBranchDefect, sdkgo.FailureValidation, "account endpoint is invalid")
	}
	query := requestURL.Query()
	query.Set("isid", credentials.ISID)
	requestURL.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return accountStatusBranch(GetAccountStatusBranchDefect, sdkgo.FailureValidation, "account request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return accountStatusRetry(sdkgo.FailureAvailability, "account request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return accountStatusRetry(sdkgo.FailureRateLimit, "provider rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return accountStatusRetry(sdkgo.FailureAvailability, "provider temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return accountStatusBranch(GetAccountStatusBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "account response exceeded the configured limit")
	}
	if err != nil {
		return accountStatusRetry(sdkgo.FailureAvailability, "account response was interrupted", 0)
	}
	var wire struct {
		Code *int `json:"code"`
		Data *struct {
			ISID           *int64 `json:"isid"`
			TodayUsed      *int64 `json:"today_used"`
			TodayRemaining *int64 `json:"today_remaining"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		return accountStatusBranch(GetAccountStatusBranchInvalidResponse, sdkgo.FailureProtocol, "provider returned an invalid account response")
	}
	switch *wire.Code {
	case 429:
		return accountStatusRetry(sdkgo.FailureRateLimit, "provider rate limited the request", retryAfter)
	case 500:
		return accountStatusRetry(sdkgo.FailureAvailability, "provider temporarily unavailable", retryAfter)
	case 403:
		return accountStatusBranch(GetAccountStatusBranchProviderRejected, sdkgo.FailureAuthentication, "provider rejected the key")
	case 502:
		return accountStatusBranch(GetAccountStatusBranchProviderRejected, sdkgo.FailureAuthorization, "provider denied account access")
	case 200:
		if response.StatusCode != http.StatusOK || wire.Data == nil || wire.Data.ISID == nil || strconv.FormatInt(*wire.Data.ISID, 10) != credentials.ISID || wire.Data.TodayUsed == nil || *wire.Data.TodayUsed < 0 || wire.Data.TodayRemaining == nil || *wire.Data.TodayRemaining < 0 {
			return accountStatusBranch(GetAccountStatusBranchInvalidResponse, sdkgo.FailureProtocol, "provider returned invalid account fields")
		}
		output := GetAccountStatusOutput{ISID: *wire.Data.ISID, TodayUsed: *wire.Data.TodayUsed, TodayRemaining: *wire.Data.TodayRemaining}
		return sdkgo.NewQueryBranch(GetAccountStatusBranchRetrieved, output, nil, sdkgo.Receipt{})
	default:
		return accountStatusBranch(GetAccountStatusBranchProviderRejected, sdkgo.FailureProviderRejection, "provider rejected the account request with code "+strconv.Itoa(*wire.Code))
	}
}
func isValidAPIKey(value string) bool {
	if value == "" || len(value) > 512 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func isValidISID(value string) bool {
	if len(value) == 0 || len(value) > 18 || value[0] == '0' {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
func accountStatusBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetAccountStatusOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "istero", Operation: "getAccountStatus", Message: message}
	return sdkgo.NewQueryBranch(branch, GetAccountStatusOutput{}, &failure, sdkgo.Receipt{})
}
func accountStatusRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetAccountStatusOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "istero", Operation: "getAccountStatus", Message: message}
	return sdkgo.NewQueryRetry[GetAccountStatusOutput](failure, delay)
}
