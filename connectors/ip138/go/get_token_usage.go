// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ip138

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetTokenUsageInput has no input because the token identifies its own quota.
type GetTokenUsageInput struct{}

// GetTokenUsageOutput contains only documented usage and quota counts.
type GetTokenUsageOutput struct {
	// CurrentHourRequests is the number of requests in the current hour.
	CurrentHourRequests int64 `json:"currentHourRequests"`
	// HourlyAllowance is the purchased number of calls per hour.
	HourlyAllowance int64 `json:"hourlyAllowance"`
	// PackageQuota is the package call quota reported by iP138.
	PackageQuota int64 `json:"packageQuota"`
}

// GetTokenUsageOperation reads token quota without an IP lookup.
type GetTokenUsageOperation struct{ client *Client }

// Definition returns the generated token-usage contract.
func (GetTokenUsageOperation) Definition() sdkgo.QueryDefinition { return GetTokenUsageDefinition }

// Invoke reads quota and excludes the query-string token from failures and results.
func (operation GetTokenUsageOperation) Invoke(call sdkgo.Call, _ GetTokenUsageInput) sdkgo.QueryAttempt[GetTokenUsageOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidToken(credentials.Token.Reveal()) {
		return usageBranch(GetTokenUsageBranchDefect, sdkgo.FailureAuthentication, "iP138 token is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, tokenUsageEndpoint+"?token="+url.QueryEscape(credentials.Token.Reveal()), nil)
	if err != nil {
		return usageBranch(GetTokenUsageBranchDefect, sdkgo.FailureValidation, "iP138 usage request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return usageRetry(sdkgo.FailureAvailability, "iP138 usage request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return usageRetry(sdkgo.FailureRateLimit, "iP138 rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return usageRetry(sdkgo.FailureAvailability, "iP138 usage service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyUsageHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return usageBranch(GetTokenUsageBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "iP138 response exceeded the configured limit")
	}
	if err != nil {
		return usageRetry(sdkgo.FailureAvailability, "iP138 response was interrupted", 0)
	}
	var wire struct {
		Ret  string `json:"ret"`
		Data struct {
			Requests *int64 `json:"reqs"`
			Hour     *int64 `json:"hour"`
			Package  *int64 `json:"package"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Ret == "" {
		return usageBranch(GetTokenUsageBranchInvalidResponse, sdkgo.FailureProtocol, "iP138 returned invalid usage data")
	}
	if wire.Ret == "err" {
		return usageBranch(GetTokenUsageBranchProviderRejected, sdkgo.FailureProviderRejection, "iP138 rejected the usage request")
	}
	if wire.Ret != "ok" || wire.Data.Requests == nil || wire.Data.Hour == nil || wire.Data.Package == nil || *wire.Data.Requests < 0 || *wire.Data.Hour < 0 || *wire.Data.Package < 0 {
		return usageBranch(GetTokenUsageBranchInvalidResponse, sdkgo.FailureProtocol, "iP138 returned incomplete usage data")
	}
	output := GetTokenUsageOutput{CurrentHourRequests: *wire.Data.Requests, HourlyAllowance: *wire.Data.Hour, PackageQuota: *wire.Data.Package}
	return sdkgo.NewQueryBranch(GetTokenUsageBranchFound, output, nil, sdkgo.Receipt{})
}
func classifyUsageHTTPStatus(status int) sdkgo.QueryAttempt[GetTokenUsageOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return usageBranch(GetTokenUsageBranchProviderRejected, kind, "iP138 rejected usage request with HTTP "+strconv.Itoa(status))
}
func usageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetTokenUsageOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "ip138", Operation: "getTokenUsage", Message: message}
	return sdkgo.NewQueryBranch(branch, GetTokenUsageOutput{}, &failure, sdkgo.Receipt{})
}
func usageRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetTokenUsageOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "ip138", Operation: "getTokenUsage", Message: message}
	return sdkgo.NewQueryRetry[GetTokenUsageOutput](failure, delay)
}
