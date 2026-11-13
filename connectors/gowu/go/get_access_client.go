// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gowu

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetAccessClientInput requests information for the app bound to the connection.
type GetAccessClientInput struct{}

// AccessClientResult contains the app remark and cumulative API call count.
type AccessClientResult struct {
	// Remark is the provider's optional application description.
	Remark string `json:"remark"`
	// APICallCount is the cumulative API call count reported by Gowu.
	APICallCount int64 `json:"apiCallCount"`
}

// GetAccessClientOperation reads the authorized application's information.
type GetAccessClientOperation struct{ client *Client }

// Definition returns the generated application-info Query contract.
func (GetAccessClientOperation) Definition() sdkgo.QueryDefinition { return GetAccessClientDefinition }

// Invoke signs one read request with a fresh nonce and filters the App Key from output.
func (operation GetAccessClientOperation) Invoke(call sdkgo.Call, _ GetAccessClientInput) sdkgo.QueryAttempt[AccessClientResult] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validAppKey(credentials.AppKey) || !validCredential(credentials.AppSecret.Reveal()) {
		return accessBranch(GetAccessClientBranchDefect, sdkgo.FailureAuthentication, "Gowu credentials are unavailable or invalid")
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return accessRetry(sdkgo.FailureAvailability, "Gowu request nonce is unavailable", 0)
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := hex.EncodeToString(nonceBytes)
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/v1/access-clients/me", nil)
	if err != nil {
		return accessBranch(GetAccessClientBranchDefect, sdkgo.FailureValidation, "Gowu application request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("x-app-key", credentials.AppKey)
	request.Header.Set("x-timestamp", timestamp)
	request.Header.Set("x-nonce", nonce)
	request.Header.Set("x-sign-version", "2")
	request.Header.Set("x-signature", signAccessClient(credentials.AppSecret.Reveal(), timestamp, nonce))
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return accessRetry(sdkgo.FailureAvailability, "Gowu application request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return accessRetry(sdkgo.FailureRateLimit, "Gowu rate limited application query", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return accessRetry(sdkgo.FailureAvailability, "Gowu application API is unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return accessBranch(GetAccessClientBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Gowu response exceeded the configured limit")
	}
	if err != nil {
		return accessRetry(sdkgo.FailureAvailability, "Gowu application response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return accessBranch(GetAccessClientBranchProviderRejected, classifyAccessHTTP(response.StatusCode), "Gowu rejected application query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var result struct {
		AppKey       string  `json:"appKey"`
		Remark       *string `json:"remark"`
		APICallCount *int64  `json:"apiCallCount"`
	}
	if json.Unmarshal(body, &result) != nil || result.AppKey != credentials.AppKey || result.APICallCount == nil || *result.APICallCount < 0 {
		return accessBranch(GetAccessClientBranchInvalidResponse, sdkgo.FailureProtocol, "Gowu returned an invalid application response")
	}
	output := AccessClientResult{APICallCount: *result.APICallCount}
	if result.Remark != nil {
		if len(*result.Remark) > 256 {
			return accessBranch(GetAccessClientBranchInvalidResponse, sdkgo.FailureProtocol, "Gowu returned an invalid application remark")
		}
		output.Remark = *result.Remark
	}
	return sdkgo.NewQueryBranch(GetAccessClientBranchFound, output, nil, sdkgo.Receipt{})
}

func signAccessClient(secret, timestamp, nonce string) string {
	emptyBody := sha256.Sum256(nil)
	canonical := strings.Join([]string{"OCP-SIGN-V2", timestamp, nonce, "GET", "/v1/access-clients/me", "", hex.EncodeToString(emptyBody[:]), ""}, "\n")
	signer := hmac.New(sha256.New, []byte(secret))
	_, _ = signer.Write([]byte(canonical))
	return hex.EncodeToString(signer.Sum(nil))
}
func validAppKey(value string) bool {
	return value != "" && len(value) <= 128 && strings.TrimSpace(value) == value
}
func validCredential(value string) bool {
	return value != "" && len(value) <= 2048 && strings.TrimSpace(value) == value
}
func classifyAccessHTTP(status int) sdkgo.FailureKind {
	switch status {
	case 401:
		return sdkgo.FailureAuthentication
	case 403:
		return sdkgo.FailureAuthorization
	case 400, 404, 412:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func accessBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[AccessClientResult] {
	failure := sdkgo.Failure{Kind: kind, Provider: "gowu", Operation: "getAccessClient", Message: message}
	return sdkgo.NewQueryBranch(branch, AccessClientResult{}, &failure, sdkgo.Receipt{})
}
func accessRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[AccessClientResult] {
	failure := sdkgo.Failure{Kind: kind, Provider: "gowu", Operation: "getAccessClient", Message: message}
	return sdkgo.NewQueryRetry[AccessClientResult](failure, delay)
}
