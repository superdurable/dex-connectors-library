// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fanyigou

import (
	"crypto/rand"
	"encoding/hex"
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

// GetAccountBalanceInput needs no operation-specific values.
type GetAccountBalanceInput struct{}

// GetAccountBalanceOutput contains document-translation resource balance.
type GetAccountBalanceOutput struct {
	// DeductType is 0 for points or 1 for pages.
	DeductType int `json:"deductType"`
	// Balance is the remaining quantity in the units selected by DeductType.
	Balance int64 `json:"balance"`
}

// GetAccountBalanceOperation queries a document-translation resource package.
type GetAccountBalanceOperation struct{ client *Client }

// Definition returns the generated account-balance Query contract.
func (GetAccountBalanceOperation) Definition() sdkgo.QueryDefinition {
	return GetAccountBalanceDefinition
}

// Invoke sends a private-key authorized read request for package type 1.
func (operation GetAccountBalanceOperation) Invoke(call sdkgo.Call, _ GetAccountBalanceInput) sdkgo.QueryAttempt[GetAccountBalanceOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.AppID.Reveal()) || !isValidCredential(credentials.PrivateKey.Reveal()) {
		return accountBranch(GetAccountBalanceBranchDefect, sdkgo.FailureAuthentication, "Fanyigou application credentials are unavailable or invalid")
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return accountRetry(sdkgo.FailureAvailability, "Fanyigou request nonce is unavailable", 0)
	}
	form := url.Values{"appid": {credentials.AppID.Reveal()}, "nonce_str": {hex.EncodeToString(nonce)}, "type": {"1"}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, accountBalanceEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return accountBranch(GetAccountBalanceBranchDefect, sdkgo.FailureValidation, "Fanyigou account request is invalid")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.PrivateKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return accountRetry(sdkgo.FailureAvailability, "Fanyigou account request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryDelay := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return accountRetry(sdkgo.FailureRateLimit, "Fanyigou rate limited account request", retryDelay)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return accountRetry(sdkgo.FailureAvailability, "Fanyigou API is temporarily unavailable", retryDelay)
	}
	if response.StatusCode != http.StatusOK {
		return accountBranch(GetAccountBalanceBranchProviderRejected, sdkgo.FailureProviderRejection, "Fanyigou rejected account request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	content, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return accountBranch(GetAccountBalanceBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Fanyigou account response exceeded configured limit")
	}
	if err != nil {
		return accountRetry(sdkgo.FailureAvailability, "Fanyigou account response was interrupted", 0)
	}
	var wire struct {
		Code *int `json:"code"`
		Data *struct {
			DeductType *int   `json:"deductType"`
			Balance    *int64 `json:"balance"`
		} `json:"data"`
	}
	if json.Unmarshal(content, &wire) != nil || wire.Code == nil {
		return accountBranch(GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "Fanyigou returned malformed account data")
	}
	switch *wire.Code {
	case 100:
	case 102, 105:
		return accountRetry(sdkgo.FailureAvailability, "Fanyigou account service is unavailable", retryDelay)
	case 108:
		return accountRetry(sdkgo.FailureRateLimit, "Fanyigou rate limited account request", retryDelay)
	default:
		return accountBranch(GetAccountBalanceBranchProviderRejected, sdkgo.FailureProviderRejection, "Fanyigou rejected account request with code "+strconv.Itoa(*wire.Code))
	}
	if wire.Data == nil || wire.Data.DeductType == nil || wire.Data.Balance == nil || (*wire.Data.DeductType != 0 && *wire.Data.DeductType != 1) || *wire.Data.Balance < 0 {
		return accountBranch(GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "Fanyigou omitted a valid document balance")
	}
	return sdkgo.NewQueryBranch(GetAccountBalanceBranchFound, GetAccountBalanceOutput{DeductType: *wire.Data.DeductType, Balance: *wire.Data.Balance}, nil, sdkgo.Receipt{})
}

func accountBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetAccountBalanceOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "fanyigou", Operation: "getAccountBalance", Message: message}
	return sdkgo.NewQueryBranch(branch, GetAccountBalanceOutput{}, &failure, sdkgo.Receipt{})
}
func accountRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetAccountBalanceOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "fanyigou", Operation: "getAccountBalance", Message: message}
	return sdkgo.NewQueryRetry[GetAccountBalanceOutput](failure, delay)
}
