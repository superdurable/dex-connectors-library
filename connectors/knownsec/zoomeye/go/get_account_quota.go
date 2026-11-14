// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoomeye

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

// GetAccountQuotaInput has no arguments; the connection identifies the account.
type GetAccountQuotaInput struct{}

// GetAccountQuotaOutput contains subscription and quota fields only, without account identity or contact details.
type GetAccountQuotaOutput struct {
	// Plan is the subscription plan name.
	Plan string `json:"plan"`
	// GeneralPoints is the available ordinary point count, encoded by ZoomEye as a decimal string.
	GeneralPoints string `json:"generalPoints"`
	// ZoomEyePoints is the available entitlement point count, encoded by ZoomEye as a decimal string.
	ZoomEyePoints string `json:"zoomeyePoints"`
}

// GetAccountQuotaOperation reads subscription and remaining points.
type GetAccountQuotaOperation struct{ client *Client }

// Definition returns the generated account-quota contract.
func (GetAccountQuotaOperation) Definition() sdkgo.QueryDefinition { return GetAccountQuotaDefinition }

// Invoke reads the connected account quota without returning personal profile fields.
func (operation GetAccountQuotaOperation) Invoke(call sdkgo.Call, _ GetAccountQuotaInput) sdkgo.QueryAttempt[GetAccountQuotaOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return accountQuotaBranch(GetAccountQuotaBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/v2/userinfo", nil)
	if err != nil {
		return accountQuotaBranch(GetAccountQuotaBranchDefect, sdkgo.FailureValidation, "account request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("API-KEY", credentials.APIKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return accountQuotaRetry(sdkgo.FailureAvailability, "ZoomEye request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return accountQuotaRetry(sdkgo.FailureRateLimit, "ZoomEye rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return accountQuotaRetry(sdkgo.FailureAvailability, "ZoomEye is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return accountQuotaBranch(GetAccountQuotaBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "ZoomEye response exceeded the configured limit")
	}
	if err != nil {
		return accountQuotaRetry(sdkgo.FailureAvailability, "ZoomEye response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return accountQuotaBranch(GetAccountQuotaBranchProviderRejected, kind, "ZoomEye rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		Code *int `json:"code"`
		Data *struct {
			Subscription *struct {
				Plan          string `json:"plan"`
				Points        string `json:"points"`
				ZoomEyePoints string `json:"zoomeye_points"`
			} `json:"subscription"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		return accountQuotaBranch(GetAccountQuotaBranchInvalidResponse, sdkgo.FailureProtocol, "ZoomEye returned an invalid response")
	}
	if *wire.Code != 60000 {
		return accountQuotaBranch(GetAccountQuotaBranchProviderRejected, sdkgo.FailureProviderRejection, "ZoomEye rejected the account request")
	}
	if wire.Data == nil || wire.Data.Subscription == nil || wire.Data.Subscription.Plan == "" || !isDecimalPoints(wire.Data.Subscription.Points) || !isDecimalPoints(wire.Data.Subscription.ZoomEyePoints) {
		return accountQuotaBranch(GetAccountQuotaBranchInvalidResponse, sdkgo.FailureProtocol, "ZoomEye response lacks subscription fields")
	}
	subscription := wire.Data.Subscription
	output := GetAccountQuotaOutput{Plan: subscription.Plan, GeneralPoints: subscription.Points, ZoomEyePoints: subscription.ZoomEyePoints}
	return sdkgo.NewQueryBranch(GetAccountQuotaBranchFetched, output, nil, sdkgo.Receipt{})
}
func isValidAPIKey(value string) bool {
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
func isDecimalPoints(value string) bool {
	if value == "" || len(value) > 30 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
func accountQuotaBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetAccountQuotaOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "zoomeye", Operation: "getAccountQuota", Message: message}
	return sdkgo.NewQueryBranch(branch, GetAccountQuotaOutput{}, &failure, sdkgo.Receipt{})
}
func accountQuotaRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetAccountQuotaOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "zoomeye", Operation: "getAccountQuota", Message: message}
	return sdkgo.NewQueryRetry[GetAccountQuotaOutput](failure, delay)
}
