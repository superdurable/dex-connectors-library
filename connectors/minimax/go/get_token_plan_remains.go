// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package minimax

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetTokenPlanRemainsInput requests the current Token Plan usage for the connection's key.
type GetTokenPlanRemainsInput struct{}

// TokenPlanModelGroup is one provider-reported quota group.
type TokenPlanModelGroup struct {
	// ModelName is the provider's group label, not necessarily a callable model ID.
	ModelName string `json:"modelName"`
	// IntervalRemainingPercent is MiniMax's 5-hour window percentage when supplied.
	IntervalRemainingPercent *float64 `json:"intervalRemainingPercent,omitempty"`
	// WeeklyRemainingPercent is MiniMax's weekly window percentage when supplied.
	WeeklyRemainingPercent *float64 `json:"weeklyRemainingPercent,omitempty"`
	// IntervalResetAtMillis is the 5-hour window end as Unix milliseconds.
	IntervalResetAtMillis int64 `json:"intervalResetAtMillis"`
	// WeeklyResetAtMillis is the weekly window end as Unix milliseconds.
	WeeklyResetAtMillis int64 `json:"weeklyResetAtMillis"`
	// IntervalStatus is MiniMax's raw 5-hour window status when supplied.
	IntervalStatus *int `json:"intervalStatus,omitempty"`
	// WeeklyStatus is MiniMax's raw weekly window status when supplied.
	WeeklyStatus *int `json:"weeklyStatus,omitempty"`
}

// GetTokenPlanRemainsOutput contains provider percentages and reset times without inferring remaining calls.
type GetTokenPlanRemainsOutput struct {
	// ModelGroups is empty when MiniMax reports no quota groups.
	ModelGroups []TokenPlanModelGroup `json:"modelGroups"`
}

// GetTokenPlanRemainsOperation reads Token Plan usage.
type GetTokenPlanRemainsOperation struct{ client *Client }

// Definition returns the generated Token Plan usage contract.
func (GetTokenPlanRemainsOperation) Definition() sdkgo.QueryDefinition {
	return GetTokenPlanRemainsDefinition
}

// Invoke reads plan usage and classifies provider and transport outcomes.
func (operation GetTokenPlanRemainsOperation) Invoke(call sdkgo.Call, _ GetTokenPlanRemainsInput) sdkgo.QueryAttempt[GetTokenPlanRemainsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return getTokenPlanRemainsBranch(GetTokenPlanRemainsBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/v1/token_plan/remains", nil)
	if err != nil {
		return getTokenPlanRemainsBranch(GetTokenPlanRemainsBranchDefect, sdkgo.FailureValidation, "Token Plan request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return getTokenPlanRemainsRetry(sdkgo.FailureAvailability, "MiniMax request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return getTokenPlanRemainsRetry(sdkgo.FailureRateLimit, "MiniMax rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return getTokenPlanRemainsRetry(sdkgo.FailureAvailability, "MiniMax is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return getTokenPlanRemainsBranch(GetTokenPlanRemainsBranchProviderRejected, kind, "MiniMax rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return getTokenPlanRemainsBranch(GetTokenPlanRemainsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "MiniMax response exceeded the configured limit")
	}
	if err != nil {
		return getTokenPlanRemainsRetry(sdkgo.FailureAvailability, "MiniMax response was interrupted", 0)
	}
	output, statusCode, err := decodeGetTokenPlanRemainsResponse(body)
	if err != nil {
		return getTokenPlanRemainsBranch(GetTokenPlanRemainsBranchInvalidResponse, sdkgo.FailureProtocol, "MiniMax returned an invalid Token Plan response")
	}
	if statusCode == 1002 || statusCode == 1039 {
		return getTokenPlanRemainsRetry(sdkgo.FailureRateLimit, "MiniMax rate limited the Token Plan request", retryAfter)
	}
	if statusCode != 0 {
		kind := sdkgo.FailureProviderRejection
		if statusCode == 1004 {
			kind = sdkgo.FailureAuthentication
		}
		if statusCode == 1008 {
			kind = sdkgo.FailureQuotaExhausted
		}
		return getTokenPlanRemainsBranch(GetTokenPlanRemainsBranchProviderRejected, kind, "MiniMax rejected Token Plan usage with code "+strconv.Itoa(statusCode))
	}
	return sdkgo.NewQueryBranch(GetTokenPlanRemainsBranchFetched, output, nil, sdkgo.Receipt{})
}

func decodeGetTokenPlanRemainsResponse(body []byte) (GetTokenPlanRemainsOutput, int, error) {
	var wire struct {
		BaseResp *struct {
			StatusCode *int `json:"status_code"`
		} `json:"base_resp"`
		ModelRemains []struct {
			ModelName                string   `json:"model_name"`
			IntervalRemainingPercent *float64 `json:"current_interval_remaining_percent"`
			WeeklyRemainingPercent   *float64 `json:"current_weekly_remaining_percent"`
			IntervalResetAtMillis    int64    `json:"end_time"`
			WeeklyResetAtMillis      int64    `json:"weekly_end_time"`
			IntervalStatus           *int     `json:"current_interval_status"`
			WeeklyStatus             *int     `json:"current_weekly_status"`
		} `json:"model_remains"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.BaseResp == nil || wire.BaseResp.StatusCode == nil {
		return GetTokenPlanRemainsOutput{}, 0, errors.New("invalid MiniMax envelope")
	}
	if *wire.BaseResp.StatusCode != 0 {
		return GetTokenPlanRemainsOutput{}, *wire.BaseResp.StatusCode, nil
	}
	if wire.ModelRemains == nil || len(wire.ModelRemains) > 1000 {
		return GetTokenPlanRemainsOutput{}, 0, errors.New("invalid MiniMax model groups")
	}
	output := GetTokenPlanRemainsOutput{ModelGroups: make([]TokenPlanModelGroup, 0, len(wire.ModelRemains))}
	seen := make(map[string]struct{}, len(wire.ModelRemains))
	for _, group := range wire.ModelRemains {
		if group.ModelName == "" || len(group.ModelName) > 256 || group.IntervalResetAtMillis < 0 || group.WeeklyResetAtMillis < 0 {
			return GetTokenPlanRemainsOutput{}, 0, errors.New("invalid MiniMax quota group")
		}
		if _, duplicate := seen[group.ModelName]; duplicate {
			return GetTokenPlanRemainsOutput{}, 0, errors.New("duplicate MiniMax quota group")
		}
		seen[group.ModelName] = struct{}{}
		if !isValidPercent(group.IntervalRemainingPercent) || !isValidPercent(group.WeeklyRemainingPercent) {
			return GetTokenPlanRemainsOutput{}, 0, errors.New("invalid MiniMax percentage")
		}
		output.ModelGroups = append(output.ModelGroups, TokenPlanModelGroup{ModelName: group.ModelName, IntervalRemainingPercent: group.IntervalRemainingPercent, WeeklyRemainingPercent: group.WeeklyRemainingPercent, IntervalResetAtMillis: group.IntervalResetAtMillis, WeeklyResetAtMillis: group.WeeklyResetAtMillis, IntervalStatus: group.IntervalStatus, WeeklyStatus: group.WeeklyStatus})
	}
	return output, 0, nil
}
func isValidPercent(percent *float64) bool {
	return percent == nil || (*percent >= 0 && *percent <= 1000)
}
func getTokenPlanRemainsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetTokenPlanRemainsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "minimax", Operation: "getTokenPlanRemains", Message: message}
	return sdkgo.NewQueryBranch(branch, GetTokenPlanRemainsOutput{ModelGroups: []TokenPlanModelGroup{}}, &failure, sdkgo.Receipt{})
}
func getTokenPlanRemainsRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetTokenPlanRemainsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "minimax", Operation: "getTokenPlanRemains", Message: message}
	return sdkgo.NewQueryRetry[GetTokenPlanRemainsOutput](failure, delay)
}
