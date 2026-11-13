// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tianxiagongchang

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

const accountEndpoint = "https://open.tianxiagongchang.com/open/v1/account"

// GetAccountStatusInput requests the account state associated with the configured key.
type GetAccountStatusInput struct{}

// GetAccountStatusOutput contains only application identity and credit status.
type GetAccountStatusOutput struct {
	// AppID identifies the application associated with the key.
	AppID int64 `json:"appId"`
	// CreditsBalance is the account-wide available credit balance in provider credits.
	CreditsBalance int64 `json:"creditsBalance"`
	// FrozenCredits is the credit amount currently frozen for this application.
	FrozenCredits int64 `json:"frozenCredits"`
	// IsSandbox indicates that the key is a sandbox key.
	IsSandbox bool `json:"isSandbox"`
}

// GetAccountStatusOperation reads the free Open Platform account endpoint.
type GetAccountStatusOperation struct{ client *Client }

// Definition returns the generated account-status contract.
func (GetAccountStatusOperation) Definition() sdkgo.QueryDefinition {
	return GetAccountStatusDefinition
}

// Invoke gets account status and classifies provider and transport outcomes.
func (operation GetAccountStatusOperation) Invoke(call sdkgo.Call, _ GetAccountStatusInput) sdkgo.QueryAttempt[GetAccountStatusOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return accountStatusBranch(GetAccountStatusBranchDefect, sdkgo.FailureAuthentication, "Open Platform key is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, accountEndpoint, nil)
	if err != nil {
		return accountStatusBranch(GetAccountStatusBranchDefect, sdkgo.FailureValidation, "account request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return accountStatusRetry(sdkgo.FailureAvailability, "account request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return accountStatusBranch(GetAccountStatusBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "account response exceeded the configured limit")
	}
	if err != nil {
		return accountStatusRetry(sdkgo.FailureAvailability, "account response was interrupted", 0)
	}
	var wire struct {
		Code           *int   `json:"code"`
		CreditsCharged *int64 `json:"credits_charged"`
		CreditsBalance *int64 `json:"credits_balance"`
		Data           *struct {
			AppID         *int64 `json:"app_id"`
			FrozenCredits *int64 `json:"frozen_credits"`
			IsSandbox     *bool  `json:"is_sandbox"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		if response.StatusCode == http.StatusTooManyRequests {
			return accountStatusRetry(sdkgo.FailureRateLimit, "provider rate limited the request", retryAfter)
		}
		if response.StatusCode >= 500 {
			return accountStatusRetry(sdkgo.FailureAvailability, "provider temporarily unavailable", retryAfter)
		}
		return accountStatusBranch(GetAccountStatusBranchInvalidResponse, sdkgo.FailureProtocol, "provider returned an invalid account response")
	}
	switch *wire.Code {
	case 42900:
		return accountStatusRetry(sdkgo.FailureRateLimit, "provider rate limited the request", retryAfter)
	case 50000, 50400:
		return accountStatusRetry(sdkgo.FailureAvailability, "provider temporarily unavailable", retryAfter)
	case 40100:
		return accountStatusBranch(GetAccountStatusBranchProviderRejected, sdkgo.FailureAuthentication, "provider rejected the key")
	case 40300, 42901:
		return accountStatusBranch(GetAccountStatusBranchProviderRejected, sdkgo.FailureAuthorization, "provider denied account access")
	case 0:
		if response.StatusCode != http.StatusOK || wire.CreditsCharged == nil || *wire.CreditsCharged != 0 || wire.CreditsBalance == nil || *wire.CreditsBalance < 0 || wire.Data == nil || wire.Data.AppID == nil || *wire.Data.AppID <= 0 || wire.Data.FrozenCredits == nil || *wire.Data.FrozenCredits < 0 || wire.Data.IsSandbox == nil {
			return accountStatusBranch(GetAccountStatusBranchInvalidResponse, sdkgo.FailureProtocol, "provider returned invalid account fields")
		}
		output := GetAccountStatusOutput{AppID: *wire.Data.AppID, CreditsBalance: *wire.CreditsBalance, FrozenCredits: *wire.Data.FrozenCredits, IsSandbox: *wire.Data.IsSandbox}
		return sdkgo.NewQueryBranch(GetAccountStatusBranchRetrieved, output, nil, sdkgo.Receipt{})
	default:
		return accountStatusBranch(GetAccountStatusBranchProviderRejected, sdkgo.FailureProviderRejection, "provider rejected the account request with code "+strconv.Itoa(*wire.Code))
	}
}
func isValidAPIKey(value string) bool {
	if value == "" || len(value) > 512 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func accountStatusBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetAccountStatusOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "tianxiagongchang", Operation: "getAccountStatus", Message: message}
	return sdkgo.NewQueryBranch(branch, GetAccountStatusOutput{}, &failure, sdkgo.Receipt{})
}
func accountStatusRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetAccountStatusOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "tianxiagongchang", Operation: "getAccountStatus", Message: message}
	return sdkgo.NewQueryRetry[GetAccountStatusOutput](failure, delay)
}
