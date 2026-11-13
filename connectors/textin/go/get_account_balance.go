// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textin

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

// GetAccountBalanceInput has no arguments; the connection identifies the account.
type GetAccountBalanceInput struct{}

// GetAccountBalanceOutput contains only the available T-coin amount and payment type.
type GetAccountBalanceOutput struct {
	// AvailableTCoins preserves the provider's decimal JSON number without floating-point rounding.
	AvailableTCoins string `json:"availableTCoins"`
	// PayType is 1 for prepaid and 2 for postpaid.
	PayType int `json:"payType"`
}

// GetAccountBalanceOperation reads the connected TextIn account's balance.
type GetAccountBalanceOperation struct{ client *Client }

// Definition returns the generated account-balance contract.
func (GetAccountBalanceOperation) Definition() sdkgo.QueryDefinition {
	return GetAccountBalanceDefinition
}

// Invoke reads the account balance and classifies provider and transport outcomes.
func (operation GetAccountBalanceOperation) Invoke(call sdkgo.Call, _ GetAccountBalanceInput) sdkgo.QueryAttempt[GetAccountBalanceOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidHeaderValue(credentials.AppID) || !isValidHeaderValue(credentials.SecretCode.Reveal()) {
		return accountBalanceBranch(GetAccountBalanceBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/user/finance/balance", nil)
	if err != nil {
		return accountBalanceBranch(GetAccountBalanceBranchDefect, sdkgo.FailureValidation, "balance request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("x-ti-app-id", credentials.AppID)
	request.Header.Set("x-ti-secret-code", credentials.SecretCode.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return accountBalanceRetry(sdkgo.FailureAvailability, "TextIn request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return accountBalanceRetry(sdkgo.FailureRateLimit, "TextIn rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return accountBalanceRetry(sdkgo.FailureAvailability, "TextIn is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return accountBalanceBranch(GetAccountBalanceBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "TextIn response exceeded the configured limit")
	}
	if err != nil {
		return accountBalanceRetry(sdkgo.FailureAvailability, "TextIn response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return accountBalanceBranch(GetAccountBalanceBranchProviderRejected, kind, "TextIn rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		Code *int `json:"code"`
		Data *struct {
			AvailableBalance *json.Number `json:"available_balance"`
			PayType          *int         `json:"pay_type"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		return accountBalanceBranch(GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "TextIn returned an invalid response")
	}
	switch *wire.Code {
	case 200:
	case 500:
		return accountBalanceRetry(sdkgo.FailureAvailability, "TextIn service is temporarily unavailable", 0)
	case 40101, 40102:
		return accountBalanceBranch(GetAccountBalanceBranchProviderRejected, sdkgo.FailureAuthentication, "TextIn rejected the account credentials")
	default:
		return accountBalanceBranch(GetAccountBalanceBranchProviderRejected, sdkgo.FailureProviderRejection, "TextIn rejected the balance request with code "+strconv.Itoa(*wire.Code))
	}
	if wire.Data == nil || wire.Data.AvailableBalance == nil || wire.Data.PayType == nil || !isExactJSONDecimal(wire.Data.AvailableBalance.String()) || (*wire.Data.PayType != 1 && *wire.Data.PayType != 2) {
		return accountBalanceBranch(GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "TextIn response lacks valid balance or payment type")
	}
	output := GetAccountBalanceOutput{AvailableTCoins: wire.Data.AvailableBalance.String(), PayType: *wire.Data.PayType}
	return sdkgo.NewQueryBranch(GetAccountBalanceBranchFetched, output, nil, sdkgo.Receipt{})
}

var exactJSONDecimal = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

func isExactJSONDecimal(value string) bool {
	return len(value) <= 64 && exactJSONDecimal.MatchString(value)
}
func isValidHeaderValue(value string) bool {
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
	failure := sdkgo.Failure{Kind: kind, Provider: "textin", Operation: "getAccountBalance", Message: message}
	return sdkgo.NewQueryBranch(branch, GetAccountBalanceOutput{}, &failure, sdkgo.Receipt{})
}
func accountBalanceRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetAccountBalanceOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "textin", Operation: "getAccountBalance", Message: message}
	return sdkgo.NewQueryRetry[GetAccountBalanceOutput](failure, delay)
}
