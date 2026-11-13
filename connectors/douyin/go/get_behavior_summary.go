// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package douyin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var chinaTime = time.FixedZone("CST", 8*3600)

// GetBehaviorSummaryInput selects one completed reporting date.
type GetBehaviorSummaryInput struct {
	// Date is a past date in YYYY-MM-DD format, within 90 days; yesterday is available after 10:00 China Standard Time.
	Date string `json:"date"`
}

// GetBehaviorSummaryOutput contains only aggregate mini app activity counts.
type GetBehaviorSummaryOutput struct {
	// ActiveUserCount is the number of active users in the requested interval.
	ActiveUserCount int64 `json:"activeUserCount"`
	// NewUserCount is the number of new users in the requested interval.
	NewUserCount int64 `json:"newUserCount"`
	// TotalUserCount is the reported total user count.
	TotalUserCount int64 `json:"totalUserCount"`
	// OpenCount is the number of mini app openings.
	OpenCount int64 `json:"openCount"`
	// ShareCount is the number of shares.
	ShareCount int64 `json:"shareCount"`
}

// GetBehaviorSummaryOperation reads completed daily aggregate behavior counts.
type GetBehaviorSummaryOperation struct{ client *Client }

// Definition returns the generated behavior-summary contract.
func (GetBehaviorSummaryOperation) Definition() sdkgo.QueryDefinition {
	return GetBehaviorSummaryDefinition
}

// Invoke acquires an application token and requests one China Standard Time day.
func (operation GetBehaviorSummaryOperation) Invoke(call sdkgo.Call, input GetBehaviorSummaryInput) sdkgo.QueryAttempt[GetBehaviorSummaryOutput] {
	start, err := time.ParseInLocation("2006-01-02", input.Date, chinaTime)
	nowInChina := time.Now().In(chinaTime)
	today := time.Date(nowInChina.Year(), nowInChina.Month(), nowInChina.Day(), 0, 0, 0, 0, chinaTime)
	if err != nil || start.Format("2006-01-02") != input.Date || start.Before(today.AddDate(0, 0, -90)) || start.After(latestAvailableBehaviorDate(nowInChina)) {
		return behaviorBranch(GetBehaviorSummaryBranchDefect, sdkgo.FailureValidation, "Douyin date must be a completed YYYY-MM-DD day within the previous 90 days")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidOpaqueValue(credentials.ClientKey) || !isValidOpaqueValue(credentials.ClientSecret.Reveal()) {
		return behaviorBranch(GetBehaviorSummaryBranchDefect, sdkgo.FailureAuthentication, "Douyin mini app credentials are unavailable or invalid")
	}
	token, attempt := operation.client.tokenFor(call, credentials)
	if attempt != nil {
		return *attempt
	}
	end := start.Add(24 * time.Hour).Add(-time.Second)
	query := url.Values{"start_time": {strconv.FormatInt(start.Unix(), 10)}, "end_time": {strconv.FormatInt(end.Unix(), 10)}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, behaviorEndpoint+"?"+query.Encode(), nil)
	if err != nil {
		return behaviorBranch(GetBehaviorSummaryBranchDefect, sdkgo.FailureValidation, "Douyin behavior request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("access-token", token)
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return behaviorRetry(sdkgo.FailureAvailability, "Douyin behavior request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	if attempt := classifyHTTPStatus(response.StatusCode, response.Header); attempt != nil {
		return *attempt
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return behaviorBranch(GetBehaviorSummaryBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Douyin response exceeded the configured limit")
	}
	if err != nil {
		return behaviorRetry(sdkgo.FailureAvailability, "Douyin behavior response was interrupted", 0)
	}
	var wire struct {
		ErrorNumber *int64 `json:"err_no"`
		Data        struct {
			Sum struct {
				Active *int64 `json:"active_user_num"`
				New    *int64 `json:"new_user_num"`
				Total  *int64 `json:"total_user_num"`
				Open   *int64 `json:"open_time"`
				Share  *int64 `json:"share_time"`
			} `json:"sum"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.ErrorNumber == nil {
		return behaviorBranch(GetBehaviorSummaryBranchInvalidResponse, sdkgo.FailureProtocol, "Douyin returned invalid behavior data")
	}
	switch *wire.ErrorNumber {
	case 0:
	case -1:
		return behaviorRetry(sdkgo.FailureAvailability, "Douyin analytics is temporarily unavailable", 0)
	case 28001008:
		operation.client.forgetToken(credentials)
		return behaviorRetry(sdkgo.FailureAuthentication, "Douyin token expired; renew authentication", 0)
	default:
		return behaviorBranch(GetBehaviorSummaryBranchProviderRejected, sdkgo.FailureProviderRejection, "Douyin rejected behavior query with code "+strconv.FormatInt(*wire.ErrorNumber, 10))
	}
	sum := wire.Data.Sum
	if sum.Active == nil || sum.New == nil || sum.Total == nil || sum.Open == nil || sum.Share == nil || *sum.Active < 0 || *sum.New < 0 || *sum.Total < 0 || *sum.Open < 0 || *sum.Share < 0 {
		return behaviorBranch(GetBehaviorSummaryBranchInvalidResponse, sdkgo.FailureProtocol, "Douyin returned incomplete behavior summary")
	}
	output := GetBehaviorSummaryOutput{ActiveUserCount: *sum.Active, NewUserCount: *sum.New, TotalUserCount: *sum.Total, OpenCount: *sum.Open, ShareCount: *sum.Share}
	return sdkgo.NewQueryBranch(GetBehaviorSummaryBranchFound, output, nil, sdkgo.Receipt{})
}

func (client *Client) tokenFor(call sdkgo.Call, credentials Credentials) (string, *sdkgo.QueryAttempt[GetBehaviorSummaryOutput]) {
	cacheKey := tokenCacheKey(credentials)
	client.tokenMu.Lock()
	defer client.tokenMu.Unlock()
	cached, exists := client.tokens[cacheKey]
	if exists && time.Now().Add(time.Minute).Before(cached.expiresAt) {
		return cached.value, nil
	}
	payload, err := json.Marshal(map[string]string{"grant_type": "client_credential", "client_key": credentials.ClientKey, "client_secret": credentials.ClientSecret.Reveal()})
	if err != nil {
		result := behaviorBranch(GetBehaviorSummaryBranchDefect, sdkgo.FailureValidation, "Douyin token request is invalid")
		return "", &result
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, tokenEndpoint, bytes.NewReader(payload))
	if err != nil {
		result := behaviorBranch(GetBehaviorSummaryBranchDefect, sdkgo.FailureValidation, "Douyin token request is invalid")
		return "", &result
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		result := behaviorRetry(sdkgo.FailureAvailability, "Douyin token request failed", 0)
		return "", &result
	}
	defer func() { _ = response.Body.Close() }()
	if attempt := classifyHTTPStatus(response.StatusCode, response.Header); attempt != nil {
		return "", attempt
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		result := behaviorBranch(GetBehaviorSummaryBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Douyin token response exceeded the configured limit")
		return "", &result
	}
	if err != nil {
		result := behaviorRetry(sdkgo.FailureAvailability, "Douyin token response was interrupted", 0)
		return "", &result
	}
	var wire struct {
		Data struct {
			AccessToken string `json:"access_token"`
			ErrorCode   *int64 `json:"error_code"`
			ExpiresIn   *int64 `json:"expires_in"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Data.ErrorCode == nil {
		result := behaviorBranch(GetBehaviorSummaryBranchInvalidResponse, sdkgo.FailureProtocol, "Douyin returned invalid token data")
		return "", &result
	}
	if *wire.Data.ErrorCode == 10020 {
		result := behaviorRetry(sdkgo.FailureRateLimit, "Douyin limited token requests", 0)
		return "", &result
	}
	if *wire.Data.ErrorCode != 0 {
		result := behaviorBranch(GetBehaviorSummaryBranchProviderRejected, sdkgo.FailureAuthentication, "Douyin rejected token request with code "+strconv.FormatInt(*wire.Data.ErrorCode, 10))
		return "", &result
	}
	if !isValidOpaqueValue(wire.Data.AccessToken) || wire.Data.ExpiresIn == nil || *wire.Data.ExpiresIn <= 60 || *wire.Data.ExpiresIn > 86400 {
		result := behaviorBranch(GetBehaviorSummaryBranchInvalidResponse, sdkgo.FailureProtocol, "Douyin returned incomplete token data")
		return "", &result
	}
	client.tokens[cacheKey] = cachedToken{value: wire.Data.AccessToken, expiresAt: time.Now().Add(time.Duration(*wire.Data.ExpiresIn) * time.Second)}
	return wire.Data.AccessToken, nil
}
func latestAvailableBehaviorDate(now time.Time) time.Time {
	local := now.In(chinaTime)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, chinaTime)
	if local.Hour() < 10 {
		return today.AddDate(0, 0, -2)
	}
	return today.AddDate(0, 0, -1)
}
func (client *Client) forgetToken(credentials Credentials) {
	client.tokenMu.Lock()
	delete(client.tokens, tokenCacheKey(credentials))
	client.tokenMu.Unlock()
}
func tokenCacheKey(credentials Credentials) string {
	digest := sha256.Sum256([]byte(credentials.ClientKey + "\x00" + credentials.ClientSecret.Reveal()))
	return hex.EncodeToString(digest[:])
}
func classifyHTTPStatus(status int, headers http.Header) *sdkgo.QueryAttempt[GetBehaviorSummaryOutput] {
	if status == http.StatusOK {
		return nil
	}
	retryAfter := providerhttp.ParseRetryAfter(headers.Get("Retry-After"), time.Now())
	if status == http.StatusTooManyRequests {
		result := behaviorRetry(sdkgo.FailureRateLimit, "Douyin rate limited the request", retryAfter)
		return &result
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		result := behaviorRetry(sdkgo.FailureAvailability, "Douyin service is temporarily unavailable", retryAfter)
		return &result
	}
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	result := behaviorBranch(GetBehaviorSummaryBranchProviderRejected, kind, "Douyin rejected request with HTTP "+strconv.Itoa(status))
	return &result
}
func behaviorBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetBehaviorSummaryOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "douyin", Operation: "getBehaviorSummary", Message: message}
	return sdkgo.NewQueryBranch(branch, GetBehaviorSummaryOutput{}, &failure, sdkgo.Receipt{})
}
func behaviorRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetBehaviorSummaryOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "douyin", Operation: "getBehaviorSummary", Message: message}
	return sdkgo.NewQueryRetry[GetBehaviorSummaryOutput](failure, delay)
}
