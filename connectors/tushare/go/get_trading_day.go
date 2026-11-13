// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tushare

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetTradingDayInput identifies one exchange and calendar date.
type GetTradingDayInput struct {
	// Exchange is SSE, SZSE, or BSE.
	Exchange string `json:"exchange"`
	// Date is a calendar date in YYYYMMDD format.
	Date string `json:"date"`
}

// GetTradingDayOutput contains only trading calendar fields.
type GetTradingDayOutput struct {
	// Exchange is the securities exchange code.
	Exchange string `json:"exchange"`
	// Date is the requested date in YYYYMMDD format.
	Date string `json:"date"`
	// IsOpen indicates whether the exchange traded on this date.
	IsOpen bool `json:"isOpen"`
	// PreviousTradingDate is the most recent prior trading date when supplied.
	PreviousTradingDate string `json:"previousTradingDate"`
}

// GetTradingDayOperation reads a calendar date without changing provider state.
type GetTradingDayOperation struct{ client *Client }

// Definition returns the generated trading day contract.
func (GetTradingDayOperation) Definition() sdkgo.QueryDefinition { return GetTradingDayDefinition }

// Invoke reads one date from Tushare Pro and excludes the token from results and errors.
func (operation GetTradingDayOperation) Invoke(call sdkgo.Call, input GetTradingDayInput) sdkgo.QueryAttempt[GetTradingDayOutput] {
	if input.Exchange != "SSE" && input.Exchange != "SZSE" && input.Exchange != "BSE" {
		return tradingDayBranch(GetTradingDayBranchDefect, sdkgo.FailureValidation, "Tushare requires SSE, SZSE, or BSE")
	}
	parsedDate, err := time.Parse("20060102", input.Date)
	if err != nil || parsedDate.Format("20060102") != input.Date {
		return tradingDayBranch(GetTradingDayBranchDefect, sdkgo.FailureValidation, "Tushare requires a valid YYYYMMDD date")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.Token.Reveal()) {
		return tradingDayBranch(GetTradingDayBranchDefect, sdkgo.FailureAuthentication, "Tushare token is unavailable or invalid")
	}
	requestBody, err := json.Marshal(struct {
		APIName string `json:"api_name"`
		Token   string `json:"token"`
		Params  struct {
			Exchange  string `json:"exchange"`
			StartDate string `json:"start_date"`
			EndDate   string `json:"end_date"`
		} `json:"params"`
		Fields string `json:"fields"`
	}{APIName: "trade_cal", Token: credentials.Token.Reveal(), Params: struct {
		Exchange  string `json:"exchange"`
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
	}{Exchange: input.Exchange, StartDate: input.Date, EndDate: input.Date}, Fields: "exchange,cal_date,is_open,pretrade_date"})
	if err != nil {
		return tradingDayBranch(GetTradingDayBranchDefect, sdkgo.FailureValidation, "Tushare calendar request is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, tradingCalendarEndpoint, bytes.NewReader(requestBody))
	if err != nil {
		return tradingDayBranch(GetTradingDayBranchDefect, sdkgo.FailureValidation, "Tushare calendar request is invalid")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return tradingDayRetry(sdkgo.FailureAvailability, "Tushare calendar request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return tradingDayRetry(sdkgo.FailureRateLimit, "Tushare rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return tradingDayRetry(sdkgo.FailureAvailability, "Tushare calendar service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyTradingDayHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return tradingDayBranch(GetTradingDayBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Tushare response exceeded the configured limit")
	}
	if err != nil {
		return tradingDayRetry(sdkgo.FailureAvailability, "Tushare response was interrupted", 0)
	}
	var wire struct {
		Code *int `json:"code"`
		Data *struct {
			Fields []string            `json:"fields"`
			Items  [][]json.RawMessage `json:"items"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		return tradingDayBranch(GetTradingDayBranchInvalidResponse, sdkgo.FailureProtocol, "Tushare returned invalid calendar data")
	}
	if *wire.Code == 2002 {
		return tradingDayBranch(GetTradingDayBranchProviderRejected, sdkgo.FailureAuthorization, "Tushare account lacks calendar permission")
	}
	if *wire.Code == 429 {
		return tradingDayRetry(sdkgo.FailureRateLimit, "Tushare rate limited the request", retryAfter)
	}
	if *wire.Code >= 500 && *wire.Code <= 599 {
		return tradingDayRetry(sdkgo.FailureAvailability, "Tushare calendar service is temporarily unavailable", retryAfter)
	}
	if *wire.Code != 0 {
		return tradingDayBranch(GetTradingDayBranchProviderRejected, sdkgo.FailureProviderRejection, "Tushare rejected calendar request with code "+strconv.Itoa(*wire.Code))
	}
	if wire.Data == nil || len(wire.Data.Fields) != 4 || wire.Data.Fields[0] != "exchange" || wire.Data.Fields[1] != "cal_date" || wire.Data.Fields[2] != "is_open" || wire.Data.Fields[3] != "pretrade_date" {
		return tradingDayBranch(GetTradingDayBranchInvalidResponse, sdkgo.FailureProtocol, "Tushare returned unexpected calendar fields")
	}
	if len(wire.Data.Items) == 0 {
		return tradingDayBranch(GetTradingDayBranchNoData, sdkgo.FailureNotFound, "Tushare returned no matching calendar date")
	}
	if len(wire.Data.Items) != 1 || len(wire.Data.Items[0]) != 4 {
		return tradingDayBranch(GetTradingDayBranchInvalidResponse, sdkgo.FailureProtocol, "Tushare returned unexpected calendar rows")
	}
	row := wire.Data.Items[0]
	var exchange, date, previousDate string
	var isOpen int
	if json.Unmarshal(row[0], &exchange) != nil || json.Unmarshal(row[1], &date) != nil || json.Unmarshal(row[2], &isOpen) != nil || json.Unmarshal(row[3], &previousDate) != nil || exchange != input.Exchange || date != input.Date || (isOpen != 0 && isOpen != 1) || (previousDate != "" && len(previousDate) != 8) {
		return tradingDayBranch(GetTradingDayBranchInvalidResponse, sdkgo.FailureProtocol, "Tushare returned invalid trading day")
	}
	output := GetTradingDayOutput{Exchange: exchange, Date: date, IsOpen: isOpen == 1, PreviousTradingDate: previousDate}
	return sdkgo.NewQueryBranch(GetTradingDayBranchFound, output, nil, sdkgo.Receipt{})
}

func classifyTradingDayHTTPStatus(status int) sdkgo.QueryAttempt[GetTradingDayOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return tradingDayBranch(GetTradingDayBranchProviderRejected, kind, "Tushare rejected calendar request with HTTP "+strconv.Itoa(status))
}
func tradingDayBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetTradingDayOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "tushare", Operation: "getTradingDay", Message: message}
	return sdkgo.NewQueryBranch(branch, GetTradingDayOutput{}, &failure, sdkgo.Receipt{})
}
func tradingDayRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetTradingDayOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "tushare", Operation: "getTradingDay", Message: message}
	return sdkgo.NewQueryRetry[GetTradingDayOutput](failure, delay)
}
