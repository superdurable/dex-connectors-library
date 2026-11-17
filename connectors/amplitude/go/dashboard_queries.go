// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package amplitude

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var chartIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// QueryChartInput identifies a saved chart by its URL ID.
type QueryChartInput struct {
	// ChartID is the segment after /chart/ in an Amplitude chart URL.
	ChartID string `json:"chartID"`
}

// ChartResult contains bounded provider JSON for one saved chart.
type ChartResult struct {
	// ChartID is the requested chart.
	ChartID string `json:"chartID"`
	// DataJSON is the chart's data object encoded as compact JSON; its shape depends on chart type.
	DataJSON string `json:"dataJSON"`
}

// ActiveUserCountsInput selects a date range and active or new user metric.
type ActiveUserCountsInput struct {
	// StartDate is the first included date in YYYYMMDD format.
	StartDate string `json:"startDate"`
	// EndDate is the last included date in YYYYMMDD format.
	EndDate string `json:"endDate"`
	// Metric is active or new; blank uses active.
	Metric string `json:"metric,omitempty"`
	// IntervalDays is 1, 7, or 30; zero uses 1.
	IntervalDays int `json:"intervalDays,omitempty"`
}

// ActiveUserCounts contains unsegmented user counts in provider date order.
type ActiveUserCounts struct {
	// StartDate is the requested first date.
	StartDate string `json:"startDate"`
	// EndDate is the requested last date.
	EndDate string `json:"endDate"`
	// Metric is active or new.
	Metric string `json:"metric"`
	// IntervalDays is the provider grouping interval.
	IntervalDays int `json:"intervalDays"`
	// Dates are provider date labels in YYYY-MM-DD format.
	Dates []string `json:"dates"`
	// Series are counts aligned with Dates.
	Series [][]float64 `json:"series"`
}

// QueryChartOperation reads a saved chart through the Dashboard REST API.
type QueryChartOperation struct{ client *Client }

// GetActiveUserCountsOperation reads an active or new user count series.
type GetActiveUserCountsOperation struct{ client *Client }

// QueryChart binds this Query to the client.
func (client *Client) QueryChart() QueryChartOperation { return QueryChartOperation{client} }

// GetActiveUserCounts binds this Query to the client.
func (client *Client) GetActiveUserCounts() GetActiveUserCountsOperation {
	return GetActiveUserCountsOperation{client}
}

// Definition returns the generated Query contract.
func (QueryChartOperation) Definition() sdkgo.QueryDefinition { return QueryChartDefinition }

// Definition returns the generated Query contract.
func (GetActiveUserCountsOperation) Definition() sdkgo.QueryDefinition {
	return GetActiveUserCountsDefinition
}

// Invoke reads one saved chart's bounded data JSON.
func (operation QueryChartOperation) Invoke(call sdkgo.Call, input QueryChartInput) sdkgo.QueryAttempt[ChartResult] {
	const operationID = "queryChart"
	if !chartIDPattern.MatchString(input.ChartID) {
		return dashboardBranch[ChartResult](operationID, QueryChartBranchDefect, sdkgo.FailureValidation, "Amplitude chart ID is invalid")
	}
	response := operation.client.getDashboard(call, "/api/3/chart/"+input.ChartID+"/query", nil)
	if attempt, failed := dashboardFailure[ChartResult](operationID, response, QueryChartBranchMissing, QueryChartBranchProviderRejected, QueryChartBranchInvalidResponse, QueryChartBranchDefect); failed {
		return attempt
	}
	var document struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(response.body, &document) != nil || len(document.Data) == 0 || (document.Data[0] != '{' && document.Data[0] != '[') {
		return dashboardBranch[ChartResult](operationID, QueryChartBranchInvalidResponse, sdkgo.FailureProtocol, "Amplitude returned invalid chart JSON")
	}
	var compact bytes.Buffer
	if json.Compact(&compact, document.Data) != nil {
		return dashboardBranch[ChartResult](operationID, QueryChartBranchInvalidResponse, sdkgo.FailureProtocol, "Amplitude returned invalid chart data")
	}
	return sdkgo.NewQueryBranch(QueryChartBranchFound, ChartResult{ChartID: input.ChartID, DataJSON: compact.String()}, nil, sdkgo.Receipt{})
}

// Invoke reads one unsegmented active or new user count series.
func (operation GetActiveUserCountsOperation) Invoke(call sdkgo.Call, input ActiveUserCountsInput) sdkgo.QueryAttempt[ActiveUserCounts] {
	const operationID = "getActiveUserCounts"
	if input.Metric == "" {
		input.Metric = "active"
	}
	if input.IntervalDays == 0 {
		input.IntervalDays = 1
	}
	start, startErr := time.Parse("20060102", input.StartDate)
	end, endErr := time.Parse("20060102", input.EndDate)
	if startErr != nil || endErr != nil || len(input.StartDate) != 8 || len(input.EndDate) != 8 || end.Before(start) || end.Sub(start) > 366*24*time.Hour || (input.Metric != "active" && input.Metric != "new") || (input.IntervalDays != 1 && input.IntervalDays != 7 && input.IntervalDays != 30) {
		return dashboardBranch[ActiveUserCounts](operationID, GetActiveUserCountsBranchDefect, sdkgo.FailureValidation, "Amplitude date range, metric, or interval is invalid")
	}
	parameters := url.Values{"start": {input.StartDate}, "end": {input.EndDate}, "m": {input.Metric}, "i": {strconv.Itoa(input.IntervalDays)}}
	response := operation.client.getDashboard(call, "/api/2/users", parameters)
	if attempt, failed := dashboardFailure[ActiveUserCounts](operationID, response, "", GetActiveUserCountsBranchProviderRejected, GetActiveUserCountsBranchInvalidResponse, GetActiveUserCountsBranchDefect); failed {
		return attempt
	}
	var document struct {
		Data *struct {
			XValues []string    `json:"xValues"`
			Series  [][]float64 `json:"series"`
		} `json:"data"`
	}
	if json.Unmarshal(response.body, &document) != nil || document.Data == nil || document.Data.XValues == nil || document.Data.Series == nil || len(document.Data.XValues) > 366 || len(document.Data.Series) > 100 {
		return dashboardBranch[ActiveUserCounts](operationID, GetActiveUserCountsBranchInvalidResponse, sdkgo.FailureProtocol, "Amplitude returned invalid user count JSON")
	}
	for _, date := range document.Data.XValues {
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return dashboardBranch[ActiveUserCounts](operationID, GetActiveUserCountsBranchInvalidResponse, sdkgo.FailureProtocol, "Amplitude returned invalid count date")
		}
	}
	for _, series := range document.Data.Series {
		if len(series) != len(document.Data.XValues) {
			return dashboardBranch[ActiveUserCounts](operationID, GetActiveUserCountsBranchInvalidResponse, sdkgo.FailureProtocol, "Amplitude returned mismatched count series")
		}
	}
	value := ActiveUserCounts{StartDate: input.StartDate, EndDate: input.EndDate, Metric: input.Metric, IntervalDays: input.IntervalDays, Dates: document.Data.XValues, Series: document.Data.Series}
	return sdkgo.NewQueryBranch(GetActiveUserCountsBranchCounted, value, nil, sdkgo.Receipt{})
}

func dashboardFailure[T any](operationID string, response providerResponse, missing, rejected, invalid, defect sdkgo.BranchID) (sdkgo.QueryAttempt[T], bool) {
	if errors.Is(response.err, errCredential) {
		return dashboardBranch[T](operationID, defect, sdkgo.FailureAuthentication, "Amplitude credentials are unavailable"), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[T](dashboardOperationFailure(operationID, sdkgo.FailureTransport, "Amplitude dashboard request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[T](dashboardOperationFailure(operationID, sdkgo.FailureRateLimit, "Amplitude rate limited dashboard request"), response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[T](dashboardOperationFailure(operationID, sdkgo.FailureAvailability, "Amplitude dashboard is temporarily unavailable"), response.retryAfter), true
	}
	if response.status == http.StatusNotFound && missing != "" {
		return sdkgo.NewQueryBranch(missing, *new(T), nil, sdkgo.Receipt{}), true
	}
	if response.status != http.StatusOK {
		return dashboardBranch[T](operationID, rejected, sdkgo.FailureProviderRejection, "Amplitude refused dashboard request"), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return dashboardBranch[T](operationID, invalid, sdkgo.FailureResponseTooLarge, "Amplitude dashboard response exceeded the configured limit"), true
	}
	if errors.Is(response.err, errCredentialEcho) {
		return dashboardBranch[T](operationID, invalid, sdkgo.FailureProtocol, "Amplitude dashboard response contained connection credentials"), true
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[T](dashboardOperationFailure(operationID, sdkgo.FailureTransport, "Amplitude dashboard response was interrupted"), 0), true
	}
	return sdkgo.QueryAttempt[T]{}, false
}

func dashboardOperationFailure(operationID string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amplitude", Operation: operationID, Message: message}
}

func dashboardBranch[T any](operationID string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := dashboardOperationFailure(operationID, kind, message)
	return sdkgo.NewQueryBranch(branch, *new(T), &failure, sdkgo.Receipt{})
}
