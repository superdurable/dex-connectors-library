// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package netease

import (
	"crypto/rand"
	"crypto/sha1"
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

// GetUserProfileInput selects one IM account for a read-only profile lookup.
type GetUserProfileInput struct {
	// AccountID is the account ID assigned when the IM user was registered.
	AccountID string `json:"accountId"`
}

// GetUserProfileOutput excludes private contact and demographic profile fields.
type GetUserProfileOutput struct {
	// AccountID is the IM account ID.
	AccountID string `json:"accountId"`
	// Name is the optional nickname, or empty when the user has none.
	Name string `json:"name"`
}

// GetUserProfileOperation reads one user profile.
type GetUserProfileOperation struct{ client *Client }

// Definition returns the generated profile query contract.
func (GetUserProfileOperation) Definition() sdkgo.QueryDefinition { return GetUserProfileDefinition }

// Invoke authenticates and classifies one provider request without returning private profile fields.
func (operation GetUserProfileOperation) Invoke(call sdkgo.Call, input GetUserProfileInput) sdkgo.QueryAttempt[GetUserProfileOutput] {
	if input.AccountID == "" || len(input.AccountID) > 128 || strings.TrimSpace(input.AccountID) != input.AccountID || strings.ContainsAny(input.AccountID, "/?#") {
		return userProfileBranch(GetUserProfileBranchDefect, sdkgo.FailureValidation, "account ID is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.AppKey.Reveal()) || !isValidCredential(credentials.AppSecret.Reveal()) {
		return userProfileBranch(GetUserProfileBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return userProfileRetry(sdkgo.FailureAvailability, "request nonce unavailable", 0)
	}
	nonce := hex.EncodeToString(nonceBytes)
	currentTime := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	signature := sha1.Sum([]byte(credentials.AppSecret.Reveal() + nonce + currentTime))
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/im/v2/users/"+url.PathEscape(input.AccountID), nil)
	if err != nil {
		return userProfileBranch(GetUserProfileBranchDefect, sdkgo.FailureValidation, "profile request is invalid")
	}
	request.Header.Set("AppKey", credentials.AppKey.Reveal())
	request.Header.Set("Nonce", nonce)
	request.Header.Set("CurTime", currentTime)
	request.Header.Set("CheckSum", hex.EncodeToString(signature[:]))
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return userProfileRetry(sdkgo.FailureAvailability, "Yunxin request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return userProfileRetry(sdkgo.FailureRateLimit, "Yunxin rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return userProfileRetry(sdkgo.FailureAvailability, "Yunxin is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return userProfileBranch(GetUserProfileBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Yunxin response exceeded the configured limit")
	}
	if err != nil {
		return userProfileRetry(sdkgo.FailureAvailability, "Yunxin response was interrupted", 0)
	}
	var wire struct {
		Code int `json:"code"`
		Data struct {
			AccountID string `json:"account_id"`
			Name      string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Code == 0 {
		return userProfileBranch(GetUserProfileBranchInvalidResponse, sdkgo.FailureProtocol, "Yunxin returned an invalid response")
	}
	switch wire.Code {
	case 200:
		if response.StatusCode != http.StatusOK || (wire.Data.AccountID != "" && wire.Data.AccountID != input.AccountID) {
			return userProfileBranch(GetUserProfileBranchInvalidResponse, sdkgo.FailureProtocol, "Yunxin returned an inconsistent profile")
		}
		return sdkgo.NewQueryBranch(GetUserProfileBranchFetched, GetUserProfileOutput{AccountID: input.AccountID, Name: wire.Data.Name}, nil, sdkgo.Receipt{})
	case 102404, 103404:
		return userProfileBranch(GetUserProfileBranchNotFound, sdkgo.FailureProviderRejection, "Yunxin account or profile does not exist")
	case 500:
		return userProfileRetry(sdkgo.FailureAvailability, "Yunxin is temporarily unavailable", retryAfter)
	case 429:
		return userProfileRetry(sdkgo.FailureRateLimit, "Yunxin rate limited the request", retryAfter)
	case 414:
		return userProfileBranch(GetUserProfileBranchProviderRejected, sdkgo.FailureAuthentication, "Yunxin rejected authentication or request parameters")
	default:
		return userProfileBranch(GetUserProfileBranchProviderRejected, sdkgo.FailureProviderRejection, "Yunxin rejected the profile request")
	}
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
func userProfileBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetUserProfileOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "netease", Operation: "getUserProfile", Message: message}
	return sdkgo.NewQueryBranch(branch, GetUserProfileOutput{}, &failure, sdkgo.Receipt{})
}
func userProfileRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetUserProfileOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "netease", Operation: "getUserProfile", Message: message}
	return sdkgo.NewQueryRetry[GetUserProfileOutput](failure, delay)
}
