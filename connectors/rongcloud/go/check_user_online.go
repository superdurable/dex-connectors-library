// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package rongcloud

import (
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

// CheckUserOnlineInput identifies one existing RongCloud IM user.
type CheckUserOnlineInput struct {
	// UserID is the application's opaque user ID, up to 64 ASCII letters or digits.
	UserID string `json:"userId"`
}

// CheckUserOnlineOutput contains only the online-state boolean.
type CheckUserOnlineOutput struct {
	// IsOnline is true when RongCloud reports status "1".
	IsOnline bool `json:"isOnline"`
}

// CheckUserOnlineOperation reads one user's current presence.
type CheckUserOnlineOperation struct{ client *Client }

// Definition returns the generated online-status contract.
func (CheckUserOnlineOperation) Definition() sdkgo.QueryDefinition { return CheckUserOnlineDefinition }

// Invoke checks presence with a fresh signed form request.
func (operation CheckUserOnlineOperation) Invoke(call sdkgo.Call, input CheckUserOnlineInput) sdkgo.QueryAttempt[CheckUserOnlineOutput] {
	if !isValidUserID(input.UserID) {
		return checkUserOnlineBranch(CheckUserOnlineBranchDefect, sdkgo.FailureValidation, "user ID is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredentialValue(credentials.AppKey) || !isValidCredentialValue(credentials.AppSecret.Reveal()) {
		return checkUserOnlineBranch(CheckUserOnlineBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	nonce, err := createNonce()
	if err != nil {
		return checkUserOnlineBranch(CheckUserOnlineBranchDefect, sdkgo.FailureAvailability, "request nonce is unavailable")
	}
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	form := url.Values{"userId": {input.UserID}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/user/checkOnline.json", strings.NewReader(form.Encode()))
	if err != nil {
		return checkUserOnlineBranch(CheckUserOnlineBranchDefect, sdkgo.FailureValidation, "online-status request is invalid")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("App-Key", credentials.AppKey)
	request.Header.Set("Nonce", nonce)
	request.Header.Set("Timestamp", timestamp)
	request.Header.Set("Signature", signRequest(credentials.AppSecret.Reveal(), nonce, timestamp))
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return checkUserOnlineRetry(sdkgo.FailureAvailability, "RongCloud request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return checkUserOnlineRetry(sdkgo.FailureRateLimit, "RongCloud rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return checkUserOnlineRetry(sdkgo.FailureAvailability, "RongCloud is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return checkUserOnlineBranch(CheckUserOnlineBranchProviderRejected, kind, "RongCloud rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return checkUserOnlineBranch(CheckUserOnlineBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "RongCloud response exceeded the configured limit")
	}
	if err != nil {
		return checkUserOnlineRetry(sdkgo.FailureAvailability, "RongCloud response was interrupted", 0)
	}
	output, providerCode, err := decodeCheckUserOnlineResponse(body)
	if err != nil {
		return checkUserOnlineBranch(CheckUserOnlineBranchInvalidResponse, sdkgo.FailureProtocol, "RongCloud returned an invalid online-status response")
	}
	if providerCode != 200 {
		if providerCode == 1008 || providerCode == 20607 {
			return checkUserOnlineRetry(sdkgo.FailureRateLimit, "RongCloud rate limited the query", retryAfter)
		}
		if providerCode == 1050 {
			return checkUserOnlineRetry(sdkgo.FailureAvailability, "RongCloud timed out internally", retryAfter)
		}
		kind := sdkgo.FailureProviderRejection
		if providerCode == 1001 || providerCode == 1004 {
			kind = sdkgo.FailureAuthentication
		}
		return checkUserOnlineBranch(CheckUserOnlineBranchProviderRejected, kind, "RongCloud rejected the query with code "+strconv.Itoa(providerCode))
	}
	return sdkgo.NewQueryBranch(CheckUserOnlineBranchChecked, output, nil, sdkgo.Receipt{})
}
func isValidUserID(userID string) bool {
	if userID == "" || len(userID) > 64 {
		return false
	}
	for _, character := range userID {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9') {
			return false
		}
	}
	return true
}
func decodeCheckUserOnlineResponse(body []byte) (CheckUserOnlineOutput, int, error) {
	var wire struct {
		Code   *int    `json:"code"`
		Status *string `json:"status"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Code == nil {
		return CheckUserOnlineOutput{}, 0, errors.New("invalid RongCloud envelope")
	}
	if *wire.Code != 200 {
		return CheckUserOnlineOutput{}, *wire.Code, nil
	}
	if wire.Status == nil || (*wire.Status != "0" && *wire.Status != "1") {
		return CheckUserOnlineOutput{}, 0, errors.New("invalid RongCloud status")
	}
	return CheckUserOnlineOutput{IsOnline: *wire.Status == "1"}, 200, nil
}
func checkUserOnlineBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[CheckUserOnlineOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "rongcloud", Operation: "checkUserOnline", Message: message}
	return sdkgo.NewQueryBranch(branch, CheckUserOnlineOutput{}, &failure, sdkgo.Receipt{})
}
func checkUserOnlineRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[CheckUserOnlineOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "rongcloud", Operation: "checkUserOnline", Message: message}
	return sdkgo.NewQueryRetry[CheckUserOnlineOutput](failure, delay)
}
