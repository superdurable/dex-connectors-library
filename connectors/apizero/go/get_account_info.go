// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package apizero

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetAccountInfoInput has no input because the API Key identifies its own account or quota.
type GetAccountInfoInput struct{}

// GetAccountInfoOutput exposes only the documented value available to this Key scope.
type GetAccountInfoOutput struct {
	// Scope is account for a standard Key or key for a prepaid Key.
	Scope string `json:"scope"`
	// BalanceYuan is the wallet balance in yuan for a standard Key only.
	BalanceYuan *string `json:"balanceYuan,omitempty"`
	// KeyQuotaRemaining is the remaining calls for a prepaid Key only.
	KeyQuotaRemaining *int64 `json:"keyQuotaRemaining,omitempty"`
}

// GetAccountInfoOperation reads wallet or prepaid quota information.
type GetAccountInfoOperation struct{ client *Client }

// Definition returns the generated account-information contract.
func (GetAccountInfoOperation) Definition() sdkgo.QueryDefinition { return GetAccountInfoDefinition }

// Invoke reads account information without exposing provider messages or credentials.
func (operation GetAccountInfoOperation) Invoke(call sdkgo.Call, _ GetAccountInfoInput) sdkgo.QueryAttempt[GetAccountInfoOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return accountInfoBranch(GetAccountInfoBranchDefect, sdkgo.FailureAuthentication, "ApiZero API Key is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, accountInfoEndpoint, nil)
	if err != nil {
		return accountInfoBranch(GetAccountInfoBranchDefect, sdkgo.FailureValidation, "ApiZero account request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return accountInfoRetry(sdkgo.FailureAvailability, "ApiZero account request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return accountInfoBranch(GetAccountInfoBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "ApiZero response exceeded the configured limit")
	}
	if err != nil {
		return accountInfoRetry(sdkgo.FailureAvailability, "ApiZero response was interrupted", 0)
	}
	var wire struct {
		Code *int `json:"code"`
		Data struct {
			Scope   string `json:"scope"`
			Balance *struct {
				Yuan *string `json:"yuan"`
			} `json:"balance"`
			Key *struct {
				QuotaRemaining *int64 `json:"quota_remaining"`
			} `json:"key"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) == nil && wire.Code != nil {
		if *wire.Code != 0 {
			return classifyAccountInfoCode(*wire.Code, retryAfter)
		}
		if response.StatusCode != http.StatusOK {
			return accountInfoBranch(GetAccountInfoBranchInvalidResponse, sdkgo.FailureProtocol, "ApiZero returned success with unexpected HTTP status")
		}
		switch wire.Data.Scope {
		case "account":
			if wire.Data.Balance == nil || wire.Data.Balance.Yuan == nil || !isValidYuan(*wire.Data.Balance.Yuan) {
				break
			}
			return sdkgo.NewQueryBranch(GetAccountInfoBranchFound, GetAccountInfoOutput{Scope: "account", BalanceYuan: wire.Data.Balance.Yuan}, nil, sdkgo.Receipt{})
		case "key":
			if wire.Data.Key == nil || wire.Data.Key.QuotaRemaining == nil || *wire.Data.Key.QuotaRemaining < 0 {
				break
			}
			return sdkgo.NewQueryBranch(GetAccountInfoBranchFound, GetAccountInfoOutput{Scope: "key", KeyQuotaRemaining: wire.Data.Key.QuotaRemaining}, nil, sdkgo.Receipt{})
		}
		return accountInfoBranch(GetAccountInfoBranchInvalidResponse, sdkgo.FailureProtocol, "ApiZero returned incomplete account data")
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return accountInfoRetry(sdkgo.FailureRateLimit, "ApiZero rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return accountInfoRetry(sdkgo.FailureAvailability, "ApiZero service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyAccountInfoHTTPStatus(response.StatusCode)
	}
	return accountInfoBranch(GetAccountInfoBranchInvalidResponse, sdkgo.FailureProtocol, "ApiZero returned invalid account data")
}

func classifyAccountInfoCode(code int, delay time.Duration) sdkgo.QueryAttempt[GetAccountInfoOutput] {
	switch code {
	case 4029:
		return accountInfoRetry(sdkgo.FailureRateLimit, "ApiZero per-second rate limit exceeded", delay)
	case 5000, 5020, 5021, 5030:
		return accountInfoRetry(sdkgo.FailureAvailability, "ApiZero service is temporarily unavailable", delay)
	case 4011, 4015:
		return accountInfoBranch(GetAccountInfoBranchProviderRejected, sdkgo.FailureAuthentication, "ApiZero rejected the API Key")
	case 4013, 4014:
		return accountInfoBranch(GetAccountInfoBranchProviderRejected, sdkgo.FailureAuthorization, "ApiZero API Key cannot access this endpoint")
	default:
		return accountInfoBranch(GetAccountInfoBranchProviderRejected, sdkgo.FailureProviderRejection, "ApiZero rejected account request with code "+strconv.Itoa(code))
	}
}
func classifyAccountInfoHTTPStatus(status int) sdkgo.QueryAttempt[GetAccountInfoOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return accountInfoBranch(GetAccountInfoBranchProviderRejected, kind, "ApiZero rejected account request with HTTP "+strconv.Itoa(status))
}
func isValidYuan(value string) bool {
	if value == "" || len(value) > 32 {
		return false
	}
	decimalPlaces := -1
	for _, character := range value {
		if character == '.' {
			if decimalPlaces >= 0 {
				return false
			}
			decimalPlaces = 0
			continue
		}
		if character < '0' || character > '9' {
			return false
		}
		if decimalPlaces >= 0 {
			decimalPlaces++
		}
	}
	return value[0] >= '0' && value[0] <= '9' && (decimalPlaces == -1 || decimalPlaces > 0)
}
func accountInfoBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetAccountInfoOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "apizero", Operation: "getAccountInfo", Message: message}
	return sdkgo.NewQueryBranch(branch, GetAccountInfoOutput{}, &failure, sdkgo.Receipt{})
}
func accountInfoRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetAccountInfoOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "apizero", Operation: "getAccountInfo", Message: message}
	return sdkgo.NewQueryRetry[GetAccountInfoOutput](failure, delay)
}
