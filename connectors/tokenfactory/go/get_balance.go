// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tokenfactory

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetBalanceInput requires no selector; the API Key identifies the account.
type GetBalanceInput struct{}

// Balance contains the current TokenFactory balance and API Key status.
type Balance struct {
	// BalanceYuan is the account snapshot in Chinese yuan and can be negative.
	BalanceYuan float64 `json:"balanceYuan"`
	// IsActive reports whether the authenticated API Key's account is active.
	IsActive bool `json:"isActive"`
	// Unit is the fixed CNY currency code returned by TokenFactory.
	Unit string `json:"unit"`
}

// GetBalanceOperation reads a free account snapshot without changing usage or API Key state.
type GetBalanceOperation struct{ client *Client }

// Definition returns the generated get-balance contract.
func (GetBalanceOperation) Definition() sdkgo.QueryDefinition { return GetBalanceDefinition }

// Invoke queries TokenFactory and classifies provider, protocol, and transport outcomes.
func (operation GetBalanceOperation) Invoke(call sdkgo.Call, _ GetBalanceInput) sdkgo.QueryAttempt[Balance] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validAPIKey(credentials.APIKey.Reveal()) {
		return balanceBranch(GetBalanceBranchDefect, sdkgo.FailureAuthentication, "TokenFactory API Key is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/v1/user/balance", nil)
	if err != nil {
		return balanceBranch(GetBalanceBranchDefect, sdkgo.FailureValidation, "balance request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return balanceRetry(sdkgo.FailureAvailability, "TokenFactory balance request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return balanceRetry(sdkgo.FailureRateLimit, "TokenFactory rate limited the balance request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return balanceRetry(sdkgo.FailureAvailability, "TokenFactory balance service is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return balanceBranch(GetBalanceBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "TokenFactory balance response exceeded the configured limit")
	}
	if err != nil {
		return balanceRetry(sdkgo.FailureAvailability, "TokenFactory balance response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return balanceBranch(GetBalanceBranchProviderRejected, classifyBalanceHTTP(response.StatusCode), "TokenFactory rejected the balance query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	balance, err := decodeBalance(body)
	if err != nil {
		return balanceBranch(GetBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "TokenFactory returned an invalid balance response")
	}
	return sdkgo.NewQueryBranch(GetBalanceBranchRetrieved, balance, nil, sdkgo.Receipt{})
}
func decodeBalance(body []byte) (Balance, error) {
	var wire struct {
		Balance  *float64 `json:"balance"`
		IsActive *bool    `json:"is_active"`
		Unit     string   `json:"unit"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Balance == nil || wire.IsActive == nil || wire.Unit != "CNY" {
		return Balance{}, errors.New("invalid TokenFactory balance JSON")
	}
	return Balance{BalanceYuan: *wire.Balance, IsActive: *wire.IsActive, Unit: wire.Unit}, nil
}
func validAPIKey(key string) bool {
	if key == "" || len(key) > 2048 || strings.TrimSpace(key) != key || strings.HasPrefix(strings.ToLower(key), "bearer ") {
		return false
	}
	for _, character := range key {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func classifyBalanceHTTP(status int) sdkgo.FailureKind {
	switch status {
	case 401:
		return sdkgo.FailureAuthentication
	case 403:
		return sdkgo.FailureAuthorization
	case 400, 404, 405:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func balanceBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[Balance] {
	failure := sdkgo.Failure{Kind: kind, Provider: "tokenfactory", Operation: "getBalance", Message: message}
	return sdkgo.NewQueryBranch(branch, Balance{}, &failure, sdkgo.Receipt{})
}
func balanceRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[Balance] {
	failure := sdkgo.Failure{Kind: kind, Provider: "tokenfactory", Operation: "getBalance", Message: message}
	return sdkgo.NewQueryRetry[Balance](failure, delay)
}
