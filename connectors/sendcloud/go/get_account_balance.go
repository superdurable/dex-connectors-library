// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sendcloud

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetAccountBalanceInput has no arguments; the credential selects the account.
type GetAccountBalanceInput struct{}

// GetAccountBalanceOutput contains provider-reported balances and quota counters without account contact information.
type GetAccountBalanceOutput struct {
	// AvailableBalance is SendCloud's avaliableBalance decimal value, preserved without a currency assumption.
	AvailableBalance string `json:"availableBalance"`
	// Balance is SendCloud's total balance decimal value, preserved without a currency assumption.
	Balance string `json:"balance"`
	// DailyQuota is SendCloud's request allowance for the current day.
	DailyQuota int `json:"dailyQuota"`
	// TodayUsedQuota is SendCloud's used request count for the current day.
	TodayUsedQuota int `json:"todayUsedQuota"`
}

// GetAccountBalanceOperation reads the account's current balance and quota.
type GetAccountBalanceOperation struct{ client *Client }

// Definition returns the generated account-balance contract.
func (GetAccountBalanceOperation) Definition() sdkgo.QueryDefinition {
	return GetAccountBalanceDefinition
}

// Invoke reads the account's balance and classifies provider and transport outcomes.
func (operation GetAccountBalanceOperation) Invoke(call sdkgo.Call, _ GetAccountBalanceInput) sdkgo.QueryAttempt[GetAccountBalanceOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.APIUser.Reveal()) || !isValidCredential(credentials.APIKey.Reveal()) {
		return accountBalanceBranch(GetAccountBalanceBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	// SendCloud documents POST for userinfo/get; a form body keeps credentials out of the URL.
	form := url.Values{"apiUser": {credentials.APIUser.Reveal()}, "apiKey": {credentials.APIKey.Reveal()}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/apiv2/userinfo/get", strings.NewReader(form.Encode()))
	if err != nil {
		return accountBalanceBranch(GetAccountBalanceBranchDefect, sdkgo.FailureValidation, "account balance request is invalid")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return accountBalanceRetry(sdkgo.FailureAvailability, "SendCloud request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return accountBalanceRetry(sdkgo.FailureRateLimit, "SendCloud rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return accountBalanceRetry(sdkgo.FailureAvailability, "SendCloud is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return accountBalanceBranch(GetAccountBalanceBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "SendCloud response exceeded the configured limit")
	}
	if err != nil {
		return accountBalanceRetry(sdkgo.FailureAvailability, "SendCloud response was interrupted", 0)
	}
	output, statusCode, isSuccess, err := decodeAccountBalanceResponse(body)
	if err != nil {
		if response.StatusCode != http.StatusOK {
			return accountBalanceBranch(GetAccountBalanceBranchProviderRejected, sdkgo.FailureProviderRejection, "SendCloud rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
		}
		return accountBalanceBranch(GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "SendCloud returned an invalid response")
	}
	if statusCode == 50000 {
		return accountBalanceRetry(sdkgo.FailureRateLimit, "SendCloud rate limited the request", retryAfter)
	}
	if response.StatusCode != http.StatusOK || !isSuccess || statusCode != 200 {
		return accountBalanceBranch(GetAccountBalanceBranchProviderRejected, classifySendCloudStatus(statusCode), "SendCloud rejected the account balance request")
	}
	return sdkgo.NewQueryBranch(GetAccountBalanceBranchRetrieved, output, nil, sdkgo.Receipt{})
}

func decodeAccountBalanceResponse(body []byte) (GetAccountBalanceOutput, int, bool, error) {
	var wire struct {
		Result     *bool `json:"result"`
		StatusCode *int  `json:"statusCode"`
		Info       *struct {
			AvailableBalance *json.Number `json:"avaliableBalance"`
			Balance          *json.Number `json:"balance"`
			Quota            *int         `json:"quota"`
			TodayUsedQuota   *int         `json:"todayUsedQuota"`
		} `json:"info"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Result == nil || wire.StatusCode == nil {
		return GetAccountBalanceOutput{}, 0, false, errors.New("invalid SendCloud envelope")
	}
	if !*wire.Result || *wire.StatusCode != 200 {
		return GetAccountBalanceOutput{}, *wire.StatusCode, false, nil
	}
	if wire.Info == nil || wire.Info.AvailableBalance == nil || wire.Info.Balance == nil || wire.Info.Quota == nil || wire.Info.TodayUsedQuota == nil {
		return GetAccountBalanceOutput{}, 0, false, errors.New("missing balance field")
	}
	available, err := strconv.ParseFloat(wire.Info.AvailableBalance.String(), 64)
	if err != nil || math.IsNaN(available) || math.IsInf(available, 0) {
		return GetAccountBalanceOutput{}, 0, false, errors.New("invalid available balance")
	}
	balance, err := strconv.ParseFloat(wire.Info.Balance.String(), 64)
	if err != nil || math.IsNaN(balance) || math.IsInf(balance, 0) || *wire.Info.Quota < 0 || *wire.Info.TodayUsedQuota < 0 {
		return GetAccountBalanceOutput{}, 0, false, errors.New("invalid balance or quota")
	}
	return GetAccountBalanceOutput{AvailableBalance: wire.Info.AvailableBalance.String(), Balance: wire.Info.Balance.String(), DailyQuota: *wire.Info.Quota, TodayUsedQuota: *wire.Info.TodayUsedQuota}, *wire.StatusCode, true, nil
}

func isValidCredential(value string) bool {
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

func classifySendCloudStatus(statusCode int) sdkgo.FailureKind {
	switch statusCode {
	case 40005:
		return sdkgo.FailureAuthentication
	case 40018, 40025, 40026, 6001:
		return sdkgo.FailureAuthorization
	default:
		return sdkgo.FailureProviderRejection
	}
}

func accountBalanceBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetAccountBalanceOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "sendcloud", Operation: "getAccountBalance", Message: message}
	return sdkgo.NewQueryBranch(branch, GetAccountBalanceOutput{}, &failure, sdkgo.Receipt{})
}
func accountBalanceRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetAccountBalanceOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "sendcloud", Operation: "getAccountBalance", Message: message}
	return sdkgo.NewQueryRetry[GetAccountBalanceOutput](failure, delay)
}
