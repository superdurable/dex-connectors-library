// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package getui

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetDailyPushReportInput selects a historical date in YYYY-MM-DD format.
type GetDailyPushReportInput struct {
	// Date is a prior calendar date in YYYY-MM-DD format.
	Date string `json:"date"`
}

// GetDailyPushReportOutput contains aggregate push totals for one app and date.
type GetDailyPushReportOutput struct {
	// Date is the requested calendar date.
	Date string `json:"date"`
	// RequestCount is the number of push requests.
	RequestCount int64 `json:"requestCount"`
	// EligibleMessageCount is the number of messages eligible for delivery.
	EligibleMessageCount int64 `json:"eligibleMessageCount"`
	// SentCount is the number of messages sent.
	SentCount int64 `json:"sentCount"`
	// ReceivedCount is the number of messages received.
	ReceivedCount int64 `json:"receivedCount"`
	// DisplayedCount is the number displayed, or -1 when unavailable.
	DisplayedCount int64 `json:"displayedCount"`
	// ClickedCount is the number clicked.
	ClickedCount int64 `json:"clickedCount"`
}

// GetDailyPushReportOperation reads historical totals without sending messages.
type GetDailyPushReportOperation struct{ client *Client }

// Definition returns the generated daily-report contract.
func (GetDailyPushReportOperation) Definition() sdkgo.QueryDefinition {
	return GetDailyPushReportDefinition
}

// Invoke exchanges app credentials for a token, then reads one daily report.
func (operation GetDailyPushReportOperation) Invoke(call sdkgo.Call, input GetDailyPushReportInput) sdkgo.QueryAttempt[GetDailyPushReportOutput] {
	date, err := time.Parse("2006-01-02", input.Date)
	today := time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
	if err != nil || date.Format("2006-01-02") != input.Date || input.Date >= today {
		return reportBranch(GetDailyPushReportBranchDefect, sdkgo.FailureValidation, "Getui report date must be a prior YYYY-MM-DD date")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAppID(credentials.AppID) || !isValidOpaqueValue(credentials.AppKey.Reveal()) || !isValidOpaqueValue(credentials.MasterSecret.Reveal()) {
		return reportBranch(GetDailyPushReportBranchDefect, sdkgo.FailureAuthentication, "Getui app credentials are unavailable or invalid")
	}
	token, attempt := operation.client.tokenFor(call, credentials)
	if attempt != nil {
		return *attempt
	}
	endpoint := apiBase + credentials.AppID + "/report/push/date/" + input.Date
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return reportBranch(GetDailyPushReportBranchDefect, sdkgo.FailureValidation, "Getui report request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("token", token)
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return reportRetry(sdkgo.FailureAvailability, "Getui report request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	if attempt := classifyHTTPStatus(response.StatusCode, response.Header); attempt != nil {
		return *attempt
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return reportBranch(GetDailyPushReportBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Getui report exceeded the configured response limit")
	}
	if err != nil {
		return reportRetry(sdkgo.FailureAvailability, "Getui report response was interrupted", 0)
	}
	var wire struct {
		Code *int `json:"code"`
		Data map[string]struct {
			Total struct {
				RequestCount         *int64 `json:"request_num"`
				EligibleMessageCount *int64 `json:"msg_num"`
				SentCount            *int64 `json:"target_num"`
				ReceivedCount        *int64 `json:"receive_num"`
				DisplayedCount       *int64 `json:"display_num"`
				ClickedCount         *int64 `json:"click_num"`
			} `json:"total"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		return reportBranch(GetDailyPushReportBranchInvalidResponse, sdkgo.FailureProtocol, "Getui returned invalid report data")
	}
	if *wire.Code == 10001 {
		operation.client.forgetToken(credentials)
		return reportRetry(sdkgo.FailureAuthentication, "Getui token expired; renew authentication", 0)
	}
	if *wire.Code != 0 {
		return reportBranch(GetDailyPushReportBranchProviderRejected, sdkgo.FailureProviderRejection, "Getui rejected the report with code "+strconv.Itoa(*wire.Code))
	}
	if len(wire.Data) != 1 {
		return reportBranch(GetDailyPushReportBranchInvalidResponse, sdkgo.FailureProtocol, "Getui returned an incomplete daily report")
	}
	item, exists := wire.Data[input.Date]
	if !exists {
		return reportBranch(GetDailyPushReportBranchInvalidResponse, sdkgo.FailureProtocol, "Getui returned another report date")
	}
	total := item.Total
	if total.RequestCount == nil || total.EligibleMessageCount == nil || total.SentCount == nil || total.ReceivedCount == nil || total.DisplayedCount == nil || total.ClickedCount == nil || *total.RequestCount < 0 || *total.EligibleMessageCount < 0 || *total.SentCount < 0 || *total.ReceivedCount < 0 || *total.DisplayedCount < -1 || *total.ClickedCount < 0 {
		return reportBranch(GetDailyPushReportBranchInvalidResponse, sdkgo.FailureProtocol, "Getui returned incomplete report totals")
	}
	output := GetDailyPushReportOutput{Date: input.Date, RequestCount: *total.RequestCount, EligibleMessageCount: *total.EligibleMessageCount, SentCount: *total.SentCount, ReceivedCount: *total.ReceivedCount, DisplayedCount: *total.DisplayedCount, ClickedCount: *total.ClickedCount}
	return sdkgo.NewQueryBranch(GetDailyPushReportBranchFound, output, nil, sdkgo.Receipt{})
}

func (client *Client) tokenFor(call sdkgo.Call, credentials Credentials) (string, *sdkgo.QueryAttempt[GetDailyPushReportOutput]) {
	cacheKey := tokenCacheKey(credentials)
	client.tokenMu.Lock()
	defer client.tokenMu.Unlock()
	cached, exists := client.tokens[cacheKey]
	if exists && time.Now().Add(time.Minute).Before(cached.expiresAt) {
		return cached.value, nil
	}
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	payload, err := json.Marshal(map[string]string{"sign": authSign(credentials.AppKey.Reveal(), timestamp, credentials.MasterSecret.Reveal()), "timestamp": timestamp, "appkey": credentials.AppKey.Reveal()})
	if err != nil {
		result := reportBranch(GetDailyPushReportBranchDefect, sdkgo.FailureValidation, "Getui auth request is invalid")
		return "", &result
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, apiBase+credentials.AppID+"/auth", bytes.NewReader(payload))
	if err != nil {
		result := reportBranch(GetDailyPushReportBranchDefect, sdkgo.FailureValidation, "Getui auth request is invalid")
		return "", &result
	}
	request.Header.Set("Content-Type", "application/json;charset=utf-8")
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		result := reportRetry(sdkgo.FailureAvailability, "Getui auth request failed", 0)
		return "", &result
	}
	defer func() { _ = response.Body.Close() }()
	if attempt := classifyHTTPStatus(response.StatusCode, response.Header); attempt != nil {
		return "", attempt
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		result := reportBranch(GetDailyPushReportBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Getui auth exceeded the configured response limit")
		return "", &result
	}
	if err != nil {
		result := reportRetry(sdkgo.FailureAvailability, "Getui auth response was interrupted", 0)
		return "", &result
	}
	var wire struct {
		Code *int `json:"code"`
		Data struct {
			Token      string `json:"token"`
			ExpireTime string `json:"expire_time"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		result := reportBranch(GetDailyPushReportBranchInvalidResponse, sdkgo.FailureProtocol, "Getui returned invalid auth data")
		return "", &result
	}
	if *wire.Code != 0 {
		result := reportBranch(GetDailyPushReportBranchProviderRejected, sdkgo.FailureAuthentication, "Getui rejected auth with code "+strconv.Itoa(*wire.Code))
		return "", &result
	}
	expireMilliseconds, err := strconv.ParseInt(wire.Data.ExpireTime, 10, 64)
	expiry := time.UnixMilli(expireMilliseconds)
	if err != nil || !isValidOpaqueValue(wire.Data.Token) || !time.Now().Add(time.Minute).Before(expiry) {
		result := reportBranch(GetDailyPushReportBranchInvalidResponse, sdkgo.FailureProtocol, "Getui returned incomplete auth token")
		return "", &result
	}
	client.tokens[cacheKey] = cachedToken{value: wire.Data.Token, expiresAt: expiry}
	return wire.Data.Token, nil
}

func (client *Client) forgetToken(credentials Credentials) {
	client.tokenMu.Lock()
	delete(client.tokens, tokenCacheKey(credentials))
	client.tokenMu.Unlock()
}
func authSign(appKey, timestamp, masterSecret string) string {
	digest := sha256.Sum256([]byte(appKey + timestamp + masterSecret))
	return hex.EncodeToString(digest[:])
}
func tokenCacheKey(credentials Credentials) string {
	digest := sha256.Sum256([]byte(credentials.AppID + "\x00" + credentials.AppKey.Reveal() + "\x00" + credentials.MasterSecret.Reveal()))
	return hex.EncodeToString(digest[:])
}
func classifyHTTPStatus(status int, headers http.Header) *sdkgo.QueryAttempt[GetDailyPushReportOutput] {
	if status == http.StatusOK {
		return nil
	}
	retryAfter := providerhttp.ParseRetryAfter(headers.Get("Retry-After"), time.Now())
	if status == http.StatusTooManyRequests {
		result := reportRetry(sdkgo.FailureRateLimit, "Getui rate limited the request", retryAfter)
		return &result
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		result := reportRetry(sdkgo.FailureAvailability, "Getui service is temporarily unavailable", retryAfter)
		return &result
	}
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	result := reportBranch(GetDailyPushReportBranchProviderRejected, kind, "Getui rejected request with HTTP "+strconv.Itoa(status))
	return &result
}
func reportBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetDailyPushReportOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "getui", Operation: "getDailyPushReport", Message: message}
	return sdkgo.NewQueryBranch(branch, GetDailyPushReportOutput{}, &failure, sdkgo.Receipt{})
}
func reportRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetDailyPushReportOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "getui", Operation: "getDailyPushReport", Message: message}
	return sdkgo.NewQueryRetry[GetDailyPushReportOutput](failure, delay)
}
