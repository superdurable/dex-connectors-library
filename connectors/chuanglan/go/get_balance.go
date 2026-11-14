// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package chuanglan

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

// GetBalanceInput has no fields because the connected domestic SMS account identifies the balance.
type GetBalanceInput struct{}

// GetBalanceOutput reports the remaining number of domestic SMS messages.
type GetBalanceOutput struct {
	// RemainingMessages is the nonnegative message count returned by Chuanglan.
	RemainingMessages int64 `json:"remainingMessages"`
}

// GetBalanceOperation reads the domestic SMS account balance.
type GetBalanceOperation struct{ client *Client }

// Definition returns the generated balance Query contract.
func (GetBalanceOperation) Definition() sdkgo.QueryDefinition { return GetBalanceDefinition }

// Invoke sends one read-only balance lookup and classifies provider outcomes.
func (operation GetBalanceOperation) Invoke(call sdkgo.Call, _ GetBalanceInput) sdkgo.QueryAttempt[GetBalanceOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.Account.Reveal()) || !isValidCredential(credentials.Password.Reveal()) {
		return balanceBranch(GetBalanceBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	body, err := json.Marshal(struct {
		Account  string `json:"account"`
		Password string `json:"password"`
	}{credentials.Account.Reveal(), credentials.Password.Reveal()})
	if err != nil {
		return balanceBranch(GetBalanceBranchDefect, sdkgo.FailureValidation, "balance request is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/msg/balance/json", bytes.NewReader(body))
	if err != nil {
		return balanceBranch(GetBalanceBranchDefect, sdkgo.FailureValidation, "balance request is invalid")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return balanceRetry(sdkgo.FailureAvailability, "Chuanglan request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return balanceRetry(sdkgo.FailureRateLimit, "Chuanglan rate limit reached", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return balanceRetry(sdkgo.FailureAvailability, "Chuanglan is temporarily unavailable", retryAfter)
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return balanceBranch(GetBalanceBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Chuanglan response exceeded the configured limit")
	}
	if err != nil {
		return balanceRetry(sdkgo.FailureAvailability, "Chuanglan response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return balanceBranch(GetBalanceBranchProviderRejected, classifyChuanglanHTTPStatus(response.StatusCode), "Chuanglan rejected the balance lookup")
	}
	var wire struct {
		Code    json.RawMessage `json:"code"`
		Balance string          `json:"balance"`
	}
	if err := json.Unmarshal(responseBody, &wire); err != nil || len(wire.Code) == 0 {
		return balanceBranch(GetBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "Chuanglan returned an invalid balance response")
	}
	code := strings.Trim(string(wire.Code), `"`)
	if code != "0" {
		if _, err := strconv.ParseInt(code, 10, 64); err != nil {
			return balanceBranch(GetBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "Chuanglan returned an invalid status code")
		}
		return balanceBranch(GetBalanceBranchProviderRejected, sdkgo.FailureProviderRejection, "Chuanglan rejected the balance lookup")
	}
	remainingMessages, err := strconv.ParseInt(wire.Balance, 10, 64)
	if err != nil || remainingMessages < 0 {
		return balanceBranch(GetBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "Chuanglan returned an invalid remaining-message count")
	}
	return sdkgo.NewQueryBranch(GetBalanceBranchFetched, GetBalanceOutput{RemainingMessages: remainingMessages}, nil, sdkgo.Receipt{})
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
func classifyChuanglanHTTPStatus(statusCode int) sdkgo.FailureKind {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return sdkgo.FailureAuthentication
	case http.StatusBadRequest:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func balanceBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetBalanceOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "chuanglan", Operation: "getBalance", Message: message}
	return sdkgo.NewQueryBranch(branch, GetBalanceOutput{}, &failure, sdkgo.Receipt{})
}
func balanceRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetBalanceOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "chuanglan", Operation: "getBalance", Message: message}
	return sdkgo.NewQueryRetry[GetBalanceOutput](failure, delay)
}
