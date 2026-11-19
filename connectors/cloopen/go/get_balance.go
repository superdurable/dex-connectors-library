// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cloopen

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetBalanceInput has no fields because the connected main account identifies the balance.
type GetBalanceInput struct{}

// GetBalanceOutput reports the provider's account balance without changing its units.
type GetBalanceOutput struct {
	// Balance is Cloopen's nonnegative decimal balance string in provider units.
	Balance string `json:"balance"`
}

// GetBalanceOperation reads the main account balance.
type GetBalanceOperation struct{ client *Client }

// Definition returns the generated balance Query contract.
func (GetBalanceOperation) Definition() sdkgo.QueryDefinition { return GetBalanceDefinition }

// Invoke sends one signed read-only account lookup and classifies provider outcomes.
func (operation GetBalanceOperation) Invoke(call sdkgo.Call, _ GetBalanceInput) sdkgo.QueryAttempt[GetBalanceOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAccountSID(credentials.AccountSID.Reveal()) || !isValidAuthToken(credentials.AuthToken.Reveal()) {
		return balanceBranch(GetBalanceBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	accountSID := credentials.AccountSID.Reveal()
	timestamp := operation.client.now().UTC().Format("20060102150405")
	digest := md5.Sum([]byte(accountSID + credentials.AuthToken.Reveal() + timestamp))
	signature := strings.ToUpper(hex.EncodeToString(digest[:]))
	requestURL := operation.client.baseURL + "/2013-12-26/Accounts/" + accountSID + "/AccountInfo?sig=" + signature
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, requestURL, nil)
	if err != nil {
		return balanceBranch(GetBalanceBranchDefect, sdkgo.FailureValidation, "account request is invalid")
	}
	request.Header.Set("Authorization", base64.StdEncoding.EncodeToString([]byte(accountSID+":"+timestamp)))
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json;charset=utf-8")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return balanceRetry(sdkgo.FailureAvailability, "Cloopen request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return balanceRetry(sdkgo.FailureRateLimit, "Cloopen rate limit reached", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return balanceRetry(sdkgo.FailureAvailability, "Cloopen is temporarily unavailable", retryAfter)
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return balanceBranch(GetBalanceBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Cloopen response exceeded the configured limit")
	}
	if err != nil {
		return balanceRetry(sdkgo.FailureAvailability, "Cloopen response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return balanceBranch(GetBalanceBranchProviderRejected, classifyCloopenHTTPStatus(response.StatusCode), "Cloopen rejected the account lookup")
	}
	var wire struct {
		StatusCode string `json:"statusCode"`
		Account    *struct {
			Balance string `json:"balance"`
		} `json:"Account"`
	}
	if err := json.Unmarshal(responseBody, &wire); err != nil || wire.StatusCode == "" {
		return balanceBranch(GetBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "Cloopen returned an invalid account response")
	}
	if wire.StatusCode != "000000" {
		if wire.StatusCode == "111111" {
			return balanceRetry(sdkgo.FailureRateLimit, "Cloopen concurrency limit reached", 0)
		}
		if strings.HasPrefix(wire.StatusCode, "1110") {
			return balanceRetry(sdkgo.FailureAvailability, "Cloopen reported an internal failure", 0)
		}
		return balanceBranch(GetBalanceBranchProviderRejected, sdkgo.FailureProviderRejection, "Cloopen rejected the account lookup")
	}
	if wire.Account == nil || !isValidBalance(wire.Account.Balance) {
		return balanceBranch(GetBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "Cloopen returned an invalid balance")
	}
	return sdkgo.NewQueryBranch(GetBalanceBranchFetched, GetBalanceOutput{Balance: wire.Account.Balance}, nil, sdkgo.Receipt{})
}

var balancePattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)

func isValidBalance(balance string) bool {
	return len(balance) <= 64 && balancePattern.MatchString(balance)
}
func isValidAccountSID(accountSID string) bool {
	if len(accountSID) != 32 {
		return false
	}
	for _, character := range accountSID {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9')) {
			return false
		}
	}
	return true
}
func isValidAuthToken(token string) bool {
	if len(token) != 32 {
		return false
	}
	for _, character := range token {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func classifyCloopenHTTPStatus(statusCode int) sdkgo.FailureKind {
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
	failure := sdkgo.Failure{Kind: kind, Provider: "cloopen", Operation: "getBalance", Message: message}
	return sdkgo.NewQueryBranch(branch, GetBalanceOutput{}, &failure, sdkgo.Receipt{})
}
func balanceRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetBalanceOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "cloopen", Operation: "getBalance", Message: message}
	return sdkgo.NewQueryRetry[GetBalanceOutput](failure, delay)
}
