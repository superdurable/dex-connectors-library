// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dieyuyun

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetAccountBalanceInput has no arguments; the connection identifies the tenant.
type GetAccountBalanceInput struct{}

// GetAccountBalanceOutput contains only the available CNY yuan balance.
type GetAccountBalanceOutput struct {
	// AvailableCNY preserves the provider's decimal JSON number without floating-point rounding.
	AvailableCNY string `json:"availableCNY"`
}

// GetAccountBalanceOperation reads the connected Wuliang AI tenant balance.
type GetAccountBalanceOperation struct{ client *Client }

// Definition returns the generated account-balance contract.
func (GetAccountBalanceOperation) Definition() sdkgo.QueryDefinition {
	return GetAccountBalanceDefinition
}

// Invoke reads the balance and classifies provider and transport outcomes.
func (operation GetAccountBalanceOperation) Invoke(call sdkgo.Call, _ GetAccountBalanceInput) sdkgo.QueryAttempt[GetAccountBalanceOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return accountBalanceBranch(GetAccountBalanceBranchDefect, sdkgo.FailureAuthentication, "connection API Key is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/v1/balance", nil)
	if err != nil {
		return accountBalanceBranch(GetAccountBalanceBranchDefect, sdkgo.FailureValidation, "balance request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return accountBalanceRetry(sdkgo.FailureAvailability, "Wuliang AI request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return accountBalanceRetry(sdkgo.FailureRateLimit, "Wuliang AI rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return accountBalanceRetry(sdkgo.FailureAvailability, "Wuliang AI is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return accountBalanceBranch(GetAccountBalanceBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Wuliang AI response exceeded the configured limit")
	}
	if err != nil {
		return accountBalanceRetry(sdkgo.FailureAvailability, "Wuliang AI response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return accountBalanceBranch(GetAccountBalanceBranchProviderRejected, kind, "Wuliang AI rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		Object   string       `json:"object"`
		Currency string       `json:"currency"`
		Balance  *json.Number `json:"balance"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Object != "balance" || wire.Currency != "CNY" || wire.Balance == nil || !isExactJSONDecimal(wire.Balance.String()) {
		return accountBalanceBranch(GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "Wuliang AI returned invalid balance data")
	}
	return sdkgo.NewQueryBranch(GetAccountBalanceBranchFetched, GetAccountBalanceOutput{AvailableCNY: wire.Balance.String()}, nil, sdkgo.Receipt{})
}

var exactJSONDecimal = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

func isExactJSONDecimal(value string) bool {
	return len(value) <= 64 && exactJSONDecimal.MatchString(value)
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
func accountBalanceBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetAccountBalanceOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "dieyuyun", Operation: "getAccountBalance", Message: message}
	return sdkgo.NewQueryBranch(branch, GetAccountBalanceOutput{}, &failure, sdkgo.Receipt{})
}
func accountBalanceRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetAccountBalanceOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "dieyuyun", Operation: "getAccountBalance", Message: message}
	return sdkgo.NewQueryRetry[GetAccountBalanceOutput](failure, delay)
}
