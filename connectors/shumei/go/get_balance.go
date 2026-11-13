// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shumei

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetBalanceInput requires no selector; the accessKey identifies the account.
type GetBalanceInput struct{}

// Balance contains Shumei's current SaaS account balance.
type Balance struct {
	// BalanceYuan is the provider-reported amount in Chinese yuan.
	BalanceYuan float64 `json:"balanceYuan"`
}

// GetBalanceOperation reads the current account balance without changing it.
type GetBalanceOperation struct{ client *Client }

// Definition returns the generated get-balance contract.
func (GetBalanceOperation) Definition() sdkgo.QueryDefinition { return GetBalanceDefinition }

// Invoke posts a read-only balance request and classifies provider and transport outcomes.
func (operation GetBalanceOperation) Invoke(call sdkgo.Call, _ GetBalanceInput) sdkgo.QueryAttempt[Balance] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validAccessKey(credentials.AccessKey.Reveal()) {
		return balanceBranch(GetBalanceBranchDefect, sdkgo.FailureAuthentication, "Shumei accessKey is unavailable or invalid")
	}
	payload, err := json.Marshal(struct {
		AccessKey string `json:"accessKey"`
	}{AccessKey: credentials.AccessKey.Reveal()})
	if err != nil {
		return balanceBranch(GetBalanceBranchDefect, sdkgo.FailureValidation, "balance request is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/saas/balance/v1", bytes.NewReader(payload))
	if err != nil {
		return balanceBranch(GetBalanceBranchDefect, sdkgo.FailureValidation, "balance request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return balanceRetry(sdkgo.FailureAvailability, "Shumei balance request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return balanceRetry(sdkgo.FailureRateLimit, "Shumei rate limited the balance request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return balanceRetry(sdkgo.FailureAvailability, "Shumei balance service is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return balanceBranch(GetBalanceBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Shumei balance response exceeded the configured limit")
	}
	if err != nil {
		return balanceRetry(sdkgo.FailureAvailability, "Shumei balance response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return balanceBranch(GetBalanceBranchProviderRejected, classifyBalanceHTTP(response.StatusCode), "Shumei rejected the balance query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	balance, code, err := decodeBalance(body)
	if err != nil {
		return balanceBranch(GetBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "Shumei returned an invalid balance response")
	}
	if code != 1100 {
		return balanceBranch(GetBalanceBranchProviderRejected, sdkgo.FailureProviderRejection, "Shumei rejected the balance query with code "+strconv.Itoa(code))
	}
	return sdkgo.NewQueryBranch(GetBalanceBranchRetrieved, balance, nil, sdkgo.Receipt{})
}

func decodeBalance(body []byte) (Balance, int, error) {
	var wire struct {
		Code    *int `json:"code"`
		Content *struct {
			Balance *float64 `json:"balance"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Code == nil {
		return Balance{}, 0, errors.New("invalid Shumei JSON")
	}
	if *wire.Code != 1100 {
		return Balance{}, *wire.Code, nil
	}
	if wire.Content == nil || wire.Content.Balance == nil {
		return Balance{}, 0, errors.New("missing Shumei balance")
	}
	return Balance{BalanceYuan: *wire.Content.Balance}, 1100, nil
}
func validAccessKey(key string) bool {
	if key == "" || len(key) > 2048 || strings.TrimSpace(key) != key {
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
	case 400, 404:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func balanceBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[Balance] {
	failure := sdkgo.Failure{Kind: kind, Provider: "shumei", Operation: "getBalance", Message: message}
	return sdkgo.NewQueryBranch(branch, Balance{}, &failure, sdkgo.Receipt{})
}
func balanceRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[Balance] {
	failure := sdkgo.Failure{Kind: kind, Provider: "shumei", Operation: "getBalance", Message: message}
	return sdkgo.NewQueryRetry[Balance](failure, delay)
}
