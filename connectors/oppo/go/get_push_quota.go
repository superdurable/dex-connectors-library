// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oppo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetPushQuotaInput needs no operation-specific values.
type GetPushQuotaInput struct{}

// GetPushQuotaOutput contains only today's OPPO Push counters.
type GetPushQuotaOutput struct {
	// TotalCount is the number of messages allowed today.
	TotalCount int64 `json:"totalCount"`
	// PushCount is the number already sent today.
	PushCount int64 `json:"pushCount"`
	// RemainCount is the number still allowed today.
	RemainCount int64 `json:"remainCount"`
}

// GetPushQuotaOperation queries daily counters using an exchanged auth token.
type GetPushQuotaOperation struct{ client *Client }

// Definition returns the generated quota Query contract.
func (GetPushQuotaOperation) Definition() sdkgo.QueryDefinition { return GetPushQuotaDefinition }

// Invoke obtains a short-lived token, then reads today's quota.
func (operation GetPushQuotaOperation) Invoke(call sdkgo.Call, _ GetPushQuotaInput) sdkgo.QueryAttempt[GetPushQuotaOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.AppKey.Reveal()) || !isValidCredential(credentials.MasterSecret.Reveal()) {
		return quotaBranch(GetPushQuotaBranchDefect, sdkgo.FailureAuthentication, "OPPO Push credentials are unavailable or invalid")
	}
	timestamp := strconv.FormatInt(operation.client.clock().UnixMilli(), 10)
	signature := pushAuthSignature(credentials.AppKey.Reveal(), timestamp, credentials.MasterSecret.Reveal())
	authForm := url.Values{"app_key": {credentials.AppKey.Reveal()}, "timestamp": {timestamp}, "sign": {signature}}
	authRequest, err := http.NewRequestWithContext(call.Context, http.MethodPost, authEndpoint, strings.NewReader(authForm.Encode()))
	if err != nil {
		return quotaBranch(GetPushQuotaBranchDefect, sdkgo.FailureValidation, "OPPO Push auth request is invalid")
	}
	authRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	authRequest.Header.Set("Accept", "application/json")
	authBody, authStatus, retryDelay, failure := operation.readResponse(authRequest)
	if failure != nil {
		return *failure
	}
	if authStatus != http.StatusOK {
		return quotaBranch(GetPushQuotaBranchProviderRejected, sdkgo.FailureProviderRejection, "OPPO Push rejected authentication with HTTP "+strconv.Itoa(authStatus))
	}
	var authWire struct {
		Code *int `json:"code"`
		Data *struct {
			AuthToken string `json:"auth_token"`
		} `json:"data"`
	}
	if json.Unmarshal(authBody, &authWire) != nil || authWire.Code == nil {
		return quotaBranch(GetPushQuotaBranchInvalidResponse, sdkgo.FailureProtocol, "OPPO Push returned malformed authentication data")
	}
	if *authWire.Code != 0 {
		return classifyPushCode(*authWire.Code, "authentication", retryDelay)
	}
	if authWire.Data == nil || !isValidCredential(authWire.Data.AuthToken) {
		return quotaBranch(GetPushQuotaBranchInvalidResponse, sdkgo.FailureProtocol, "OPPO Push omitted an auth token")
	}
	quotaRequest, err := http.NewRequestWithContext(call.Context, http.MethodGet, pushQuotaEndpoint, nil)
	if err != nil {
		return quotaBranch(GetPushQuotaBranchDefect, sdkgo.FailureValidation, "OPPO Push quota request is invalid")
	}
	quotaRequest.Header.Set("auth_token", authWire.Data.AuthToken)
	quotaRequest.Header.Set("Accept", "application/json")
	quotaBody, quotaStatus, quotaRetryDelay, quotaFailure := operation.readResponse(quotaRequest)
	if quotaFailure != nil {
		return *quotaFailure
	}
	if quotaStatus != http.StatusOK {
		return quotaBranch(GetPushQuotaBranchProviderRejected, sdkgo.FailureProviderRejection, "OPPO Push rejected quota request with HTTP "+strconv.Itoa(quotaStatus))
	}
	var quotaWire struct {
		Code *int `json:"code"`
		Data *struct {
			TotalCount  *int64 `json:"total_count"`
			PushCount   *int64 `json:"push_count"`
			RemainCount *int64 `json:"remain_count"`
		} `json:"data"`
	}
	if json.Unmarshal(quotaBody, &quotaWire) != nil || quotaWire.Code == nil {
		return quotaBranch(GetPushQuotaBranchInvalidResponse, sdkgo.FailureProtocol, "OPPO Push returned malformed quota data")
	}
	if *quotaWire.Code != 0 {
		return classifyPushCode(*quotaWire.Code, "quota", quotaRetryDelay)
	}
	if quotaWire.Data == nil || quotaWire.Data.TotalCount == nil || quotaWire.Data.PushCount == nil || quotaWire.Data.RemainCount == nil || *quotaWire.Data.TotalCount < 0 || *quotaWire.Data.PushCount < 0 || *quotaWire.Data.RemainCount < 0 || *quotaWire.Data.PushCount+*quotaWire.Data.RemainCount > *quotaWire.Data.TotalCount {
		return quotaBranch(GetPushQuotaBranchInvalidResponse, sdkgo.FailureProtocol, "OPPO Push returned invalid quota counters")
	}
	return sdkgo.NewQueryBranch(GetPushQuotaBranchFound, GetPushQuotaOutput{
		TotalCount: *quotaWire.Data.TotalCount, PushCount: *quotaWire.Data.PushCount, RemainCount: *quotaWire.Data.RemainCount,
	}, nil, sdkgo.Receipt{})
}

func (operation GetPushQuotaOperation) readResponse(request *http.Request) ([]byte, int, time.Duration, *sdkgo.QueryAttempt[GetPushQuotaOutput]) {
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		attempt := quotaRetry(sdkgo.FailureAvailability, "OPPO Push request failed", 0)
		return nil, 0, 0, &attempt
	}
	defer func() { _ = response.Body.Close() }()
	retryDelay := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		attempt := quotaRetry(sdkgo.FailureRateLimit, "OPPO Push rate limited the request", retryDelay)
		return nil, 0, 0, &attempt
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		attempt := quotaRetry(sdkgo.FailureAvailability, "OPPO Push API is temporarily unavailable", retryDelay)
		return nil, 0, 0, &attempt
	}
	if response.StatusCode != http.StatusOK {
		return nil, response.StatusCode, retryDelay, nil
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		attempt := quotaBranch(GetPushQuotaBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "OPPO Push response exceeded configured limit")
		return nil, 0, 0, &attempt
	}
	if err != nil && !errors.Is(err, io.EOF) {
		attempt := quotaRetry(sdkgo.FailureAvailability, "OPPO Push response was interrupted", 0)
		return nil, 0, 0, &attempt
	}
	return body, response.StatusCode, retryDelay, nil
}

func pushAuthSignature(appKey, timestamp, masterSecret string) string {
	digest := sha256.Sum256([]byte(appKey + timestamp + masterSecret))
	return hex.EncodeToString(digest[:])
}

func classifyPushCode(code int, operation string, retryDelay time.Duration) sdkgo.QueryAttempt[GetPushQuotaOutput] {
	switch code {
	case -2, 13:
		return quotaRetry(sdkgo.FailureRateLimit, "OPPO Push "+operation+" is rate limited", retryDelay)
	case -1:
		return quotaRetry(sdkgo.FailureAvailability, "OPPO Push "+operation+" is unavailable", retryDelay)
	default:
		return quotaBranch(GetPushQuotaBranchProviderRejected, sdkgo.FailureProviderRejection, "OPPO Push rejected "+operation+" with code "+strconv.Itoa(code))
	}
}

func quotaBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetPushQuotaOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "oppo", Operation: "getPushQuota", Message: message}
	return sdkgo.NewQueryBranch(branch, GetPushQuotaOutput{}, &failure, sdkgo.Receipt{})
}

func quotaRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetPushQuotaOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "oppo", Operation: "getPushQuota", Message: message}
	return sdkgo.NewQueryRetry[GetPushQuotaOutput](failure, delay)
}
