// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tianapi

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

// GetHolidayStatusInput identifies one Chinese calendar date in YYYY-MM-DD format.
type GetHolidayStatusInput struct {
	// Date is a single Gregorian calendar date in YYYY-MM-DD format.
	Date string `json:"date"`
}

// GetHolidayStatusOutput contains only the requested date and official day classifications.
type GetHolidayStatusOutput struct {
	// Date is the queried calendar date.
	Date string `json:"date"`
	// DayCode is 0 for a workday, 1 for a holiday, 2 for a weekend, or 3 for a makeup workday.
	DayCode int `json:"dayCode"`
	// IsNotWork is 0 when working and 1 when resting.
	IsNotWork int `json:"isNotWork"`
}

// GetHolidayStatusOperation reads one date without modifying provider state.
type GetHolidayStatusOperation struct{ client *Client }

// Definition returns the generated holiday-status contract.
func (GetHolidayStatusOperation) Definition() sdkgo.QueryDefinition {
	return GetHolidayStatusDefinition
}

// Invoke queries one date and excludes the query-string key and raw provider data from failures.
func (operation GetHolidayStatusOperation) Invoke(call sdkgo.Call, input GetHolidayStatusInput) sdkgo.QueryAttempt[GetHolidayStatusOutput] {
	date, err := time.Parse("2006-01-02", input.Date)
	if err != nil || date.Format("2006-01-02") != input.Date {
		return holidayBranch(GetHolidayStatusBranchDefect, sdkgo.FailureValidation, "TianAPI date must be YYYY-MM-DD")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return holidayBranch(GetHolidayStatusBranchDefect, sdkgo.FailureAuthentication, "TianAPI key is unavailable or invalid")
	}
	// TianAPI documents GET and form POST; the form body keeps the key out of the URL.
	form := url.Values{"key": {credentials.APIKey.Reveal()}, "date": {input.Date}, "type": {"0"}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, holidayEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return holidayBranch(GetHolidayStatusBranchDefect, sdkgo.FailureValidation, "TianAPI holiday request is invalid")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return holidayRetry(sdkgo.FailureAvailability, "TianAPI holiday request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return holidayRetry(sdkgo.FailureRateLimit, "TianAPI rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return holidayRetry(sdkgo.FailureAvailability, "TianAPI holiday service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyHolidayHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return holidayBranch(GetHolidayStatusBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "TianAPI response exceeded the configured limit")
	}
	if err != nil {
		return holidayRetry(sdkgo.FailureAvailability, "TianAPI response was interrupted", 0)
	}
	var wire struct {
		Code   *int `json:"code"`
		Result struct {
			List []struct {
				Date      string `json:"date"`
				DayCode   *int   `json:"daycode"`
				IsNotWork *int   `json:"isnotwork"`
			} `json:"list"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		return holidayBranch(GetHolidayStatusBranchInvalidResponse, sdkgo.FailureProtocol, "TianAPI returned invalid holiday data")
	}
	switch *wire.Code {
	case 200:
	case 130:
		return holidayRetry(sdkgo.FailureRateLimit, "TianAPI request rate limit was reached", 0)
	case 100, 120:
		return holidayRetry(sdkgo.FailureAvailability, "TianAPI holiday service is temporarily unavailable", 0)
	default:
		return holidayBranch(GetHolidayStatusBranchProviderRejected, sdkgo.FailureProviderRejection, "TianAPI rejected the holiday request with code "+strconv.Itoa(*wire.Code))
	}
	if len(wire.Result.List) != 1 || wire.Result.List[0].Date != input.Date || wire.Result.List[0].DayCode == nil || wire.Result.List[0].IsNotWork == nil || *wire.Result.List[0].DayCode < 0 || *wire.Result.List[0].DayCode > 3 || (*wire.Result.List[0].IsNotWork != 0 && *wire.Result.List[0].IsNotWork != 1) {
		return holidayBranch(GetHolidayStatusBranchInvalidResponse, sdkgo.FailureProtocol, "TianAPI returned incomplete holiday status")
	}
	item := wire.Result.List[0]
	return sdkgo.NewQueryBranch(GetHolidayStatusBranchFound, GetHolidayStatusOutput{Date: item.Date, DayCode: *item.DayCode, IsNotWork: *item.IsNotWork}, nil, sdkgo.Receipt{})
}

func classifyHolidayHTTPStatus(status int) sdkgo.QueryAttempt[GetHolidayStatusOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return holidayBranch(GetHolidayStatusBranchProviderRejected, kind, "TianAPI rejected the holiday request with HTTP "+strconv.Itoa(status))
}
func holidayBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetHolidayStatusOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "tianapi", Operation: "getHolidayStatus", Message: message}
	return sdkgo.NewQueryBranch(branch, GetHolidayStatusOutput{}, &failure, sdkgo.Receipt{})
}
func holidayRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetHolidayStatusOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "tianapi", Operation: "getHolidayStatus", Message: message}
	return sdkgo.NewQueryRetry[GetHolidayStatusOutput](failure, delay)
}
