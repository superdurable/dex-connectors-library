// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tripo

import (
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// GetAccountBalanceInput has no arguments; the connection identifies the account.
type GetAccountBalanceInput struct{}

// GetAccountBalanceOutput retains exact decimal credit values from the provider's JSON numbers.
type GetAccountBalanceOutput struct {
	// AvailableCredits is the available balance as a nonnegative decimal string in credits.
	AvailableCredits string `json:"availableCredits"`
	// FrozenCredits is the reserved balance as a nonnegative decimal string in credits.
	FrozenCredits string `json:"frozenCredits"`
}

// GetAccountBalanceOperation reads available and frozen Tripo credits.
type GetAccountBalanceOperation struct{ client *Client }

// Definition returns the generated account-balance contract.
func (GetAccountBalanceOperation) Definition() sdkgo.QueryDefinition {
	return GetAccountBalanceDefinition
}

// Invoke reads the connected account's balance and classifies provider and transport outcomes.
func (operation GetAccountBalanceOperation) Invoke(call sdkgo.Call, _ GetAccountBalanceInput) sdkgo.QueryAttempt[GetAccountBalanceOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return accountBalanceBranch(GetAccountBalanceBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/v3/account/balance", nil)
	if err != nil {
		return accountBalanceBranch(GetAccountBalanceBranchDefect, sdkgo.FailureValidation, "account balance request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return accountBalanceRetry(sdkgo.FailureAvailability, "Tripo request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return accountBalanceRetry(sdkgo.FailureRateLimit, "Tripo rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return accountBalanceRetry(sdkgo.FailureAvailability, "Tripo is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return accountBalanceBranch(GetAccountBalanceBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Tripo response exceeded the configured limit")
	}
	if err != nil {
		return accountBalanceRetry(sdkgo.FailureAvailability, "Tripo response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return accountBalanceBranch(GetAccountBalanceBranchProviderRejected, kind, "Tripo rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		Code *int `json:"code"`
		Data *struct {
			Balance *json.Number `json:"balance"`
			Frozen  *json.Number `json:"frozen"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		return accountBalanceBranch(GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "Tripo returned an invalid response")
	}
	if *wire.Code != 0 {
		return classifyTripoError(*wire.Code)
	}
	if wire.Data == nil || wire.Data.Balance == nil || wire.Data.Frozen == nil || !isCreditDecimal(wire.Data.Balance.String()) || !isCreditDecimal(wire.Data.Frozen.String()) {
		return accountBalanceBranch(GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "Tripo response lacks valid credit balances")
	}
	output := GetAccountBalanceOutput{AvailableCredits: wire.Data.Balance.String(), FrozenCredits: wire.Data.Frozen.String()}
	return sdkgo.NewQueryBranch(GetAccountBalanceBranchFetched, output, nil, sdkgo.Receipt{})
}
func classifyTripoError(code int) sdkgo.QueryAttempt[GetAccountBalanceOutput] {
	switch code {
	case 1000, 1001:
		return accountBalanceRetry(sdkgo.FailureAvailability, "Tripo service is temporarily unavailable", 0)
	case 1007, 2000:
		return accountBalanceRetry(sdkgo.FailureRateLimit, "Tripo rate limited the request", 0)
	case 2, 1002:
		return accountBalanceBranch(GetAccountBalanceBranchProviderRejected, sdkgo.FailureAuthentication, "Tripo rejected the API key")
	case 2010:
		return accountBalanceBranch(GetAccountBalanceBranchProviderRejected, sdkgo.FailureQuotaExhausted, "Tripo credits are exhausted")
	default:
		return accountBalanceBranch(GetAccountBalanceBranchProviderRejected, sdkgo.FailureProviderRejection, "Tripo rejected the account request")
	}
}
func isValidAPIKey(value string) bool {
	if value == "" || len(value) > 512 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func isCreditDecimal(value string) bool {
	if value == "" || len(value) > 30 {
		return false
	}
	decimals := 0
	hasDigit := false
	hasDot := false
	for _, character := range value {
		if character == '.' {
			if hasDot || !hasDigit {
				return false
			}
			hasDot = true
			continue
		}
		if character < '0' || character > '9' {
			return false
		}
		hasDigit = true
		if hasDot {
			decimals++
			if decimals > 2 {
				return false
			}
		}
	}
	return hasDigit && (!hasDot || decimals > 0)
}
func accountBalanceBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetAccountBalanceOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "tripo", Operation: "getAccountBalance", Message: message}
	return sdkgo.NewQueryBranch(branch, GetAccountBalanceOutput{}, &failure, sdkgo.Receipt{})
}
func accountBalanceRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetAccountBalanceOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "tripo", Operation: "getAccountBalance", Message: message}
	return sdkgo.NewQueryRetry[GetAccountBalanceOutput](failure, delay)
}
