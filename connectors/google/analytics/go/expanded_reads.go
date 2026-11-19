// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package analytics4

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// RunRealtimeReportInput requests a bounded report over the last 30 minutes.
type RunRealtimeReportInput struct {
	// PropertyID is the numeric GA4 property ID.
	PropertyID string `json:"propertyId"`
	// Dimensions are up to nine realtime dimension names.
	Dimensions []string `json:"dimensions"`
	// Metrics are one to ten realtime metric names.
	Metrics []string `json:"metrics"`
	// StartMinutesAgo is the older inclusive minute, at most 29; zero with EndMinutesAgo zero uses 29.
	StartMinutesAgo int `json:"startMinutesAgo,omitempty"`
	// EndMinutesAgo is the newer inclusive minute, at most StartMinutesAgo.
	EndMinutesAgo int `json:"endMinutesAgo,omitempty"`
	// Limit is the maximum rows, 1 to 10000; zero uses 100.
	Limit int64 `json:"limit,omitempty"`
}

// ListPropertiesInput selects one Analytics account and page.
type ListPropertiesInput struct {
	// AccountID is the numeric Analytics account ID from Admin.
	AccountID string `json:"accountId"`
	// PageSize is 1 to 200; zero uses 50.
	PageSize int64 `json:"pageSize,omitempty"`
	// PageToken is the prior page's opaque cursor; blank starts at the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// PropertiesPage contains one page of accessible properties.
type PropertiesPage struct {
	// Properties are property identities and display metadata.
	Properties []Property `json:"properties"`
	// NextPageToken is Google's opaque cursor; blank ends pagination.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// ListDataStreamsInput selects one property and stream page.
type ListDataStreamsInput struct {
	// PropertyID is the numeric GA4 property ID.
	PropertyID string `json:"propertyId"`
	// PageSize is 1 to 200; zero uses 50.
	PageSize int64 `json:"pageSize,omitempty"`
	// PageToken is the prior page's opaque cursor; blank starts at the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// DataStream contains non-secret GA4 stream identity and display metadata.
type DataStream struct {
	// Name is properties/{property}/dataStreams/{stream}.
	Name string `json:"name"`
	// DisplayName is the stream name in Analytics Admin.
	DisplayName string `json:"displayName"`
	// Type identifies WEB_DATA_STREAM, IOS_APP_DATA_STREAM, or ANDROID_APP_DATA_STREAM.
	Type string `json:"type"`
}

// DataStreamsPage contains one page of property data streams.
type DataStreamsPage struct {
	// DataStreams are returned stream identities.
	DataStreams []DataStream `json:"dataStreams"`
	// NextPageToken is Google's opaque cursor; blank ends pagination.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// RunRealtimeReportOperation reads current GA4 event totals.
type RunRealtimeReportOperation struct{ client *Client }

// ListPropertiesOperation lists properties under an Analytics account.
type ListPropertiesOperation struct{ client *Client }

// ListDataStreamsOperation lists streams under a GA4 property.
type ListDataStreamsOperation struct{ client *Client }

// RunRealtimeReport binds the realtime report Query to this client.
func (client *Client) RunRealtimeReport() RunRealtimeReportOperation {
	return RunRealtimeReportOperation{client}
}

// ListProperties binds the property listing Query to this client.
func (client *Client) ListProperties() ListPropertiesOperation {
	return ListPropertiesOperation{client}
}

// ListDataStreams binds the data stream listing Query to this client.
func (client *Client) ListDataStreams() ListDataStreamsOperation {
	return ListDataStreamsOperation{client}
}

// Definition returns the generated realtime report contract.
func (RunRealtimeReportOperation) Definition() sdkgo.QueryDefinition {
	return RunRealtimeReportDefinition
}

// Definition returns the generated property listing contract.
func (ListPropertiesOperation) Definition() sdkgo.QueryDefinition { return ListPropertiesDefinition }

// Definition returns the generated stream listing contract.
func (ListDataStreamsOperation) Definition() sdkgo.QueryDefinition { return ListDataStreamsDefinition }

// Invoke runs a bounded realtime report with a fixed minute range.
func (operation RunRealtimeReportOperation) Invoke(call sdkgo.Call, input RunRealtimeReportInput) sdkgo.QueryAttempt[ReportPage] {
	if !validPropertyID(input.PropertyID) || !validNames(input.Dimensions, maxReportDimensions, false) || !validNames(input.Metrics, maxReportMetrics, true) || input.StartMinutesAgo < 0 || input.StartMinutesAgo > 29 || input.EndMinutesAgo < 0 || input.EndMinutesAgo > input.StartMinutesAgo || input.Limit < 0 || input.Limit > 10000 {
		return queryBranch("runRealtimeReport", RunRealtimeReportBranchDefect, ReportPage{}, sdkgo.FailureValidation, "realtime report input is invalid")
	}
	startMinutesAgo := input.StartMinutesAgo
	if startMinutesAgo == 0 && input.EndMinutesAgo == 0 {
		startMinutesAgo = 29
	}
	limit := input.Limit
	if limit == 0 {
		limit = 100
	}
	dimensions := make([]map[string]string, 0, len(input.Dimensions))
	for _, name := range input.Dimensions {
		dimensions = append(dimensions, map[string]string{"name": name})
	}
	metrics := make([]map[string]string, 0, len(input.Metrics))
	for _, name := range input.Metrics {
		metrics = append(metrics, map[string]string{"name": name})
	}
	body, _ := json.Marshal(map[string]any{"dimensions": dimensions, "metrics": metrics, "minuteRanges": []map[string]int{{"startMinutesAgo": startMinutesAgo, "endMinutesAgo": input.EndMinutesAgo}}, "limit": strconv.FormatInt(limit, 10)})
	response := operation.client.request(call, operation.client.dataBaseURL, http.MethodPost, "/v1beta/properties/"+input.PropertyID+":runRealtimeReport", body)
	if decision, failed := classifyRead[ReportPage](response, "runRealtimeReport", readBranches{rejected: RunRealtimeReportBranchProviderRejected, invalid: RunRealtimeReportBranchInvalidResponse, defect: RunRealtimeReportBranchDefect}); failed {
		return decision
	}
	page, err := decodeRealtimeReport(response.body, len(input.Dimensions), len(input.Metrics), limit)
	if err != nil {
		return queryBranch("runRealtimeReport", RunRealtimeReportBranchInvalidResponse, ReportPage{}, sdkgo.FailureProtocol, "Google returned invalid realtime report JSON")
	}
	return sdkgo.NewQueryBranch(RunRealtimeReportBranchReported, page, nil, sdkgo.Receipt{})
}

// Invoke lists one bounded page of accessible account properties.
func (operation ListPropertiesOperation) Invoke(call sdkgo.Call, input ListPropertiesInput) sdkgo.QueryAttempt[PropertiesPage] {
	if !validPropertyID(input.AccountID) || !validAnalyticsPage(input.PageSize, input.PageToken) {
		return queryBranch("listProperties", ListPropertiesBranchDefect, PropertiesPage{}, sdkgo.FailureValidation, "account or page input is invalid")
	}
	pageSize := analyticsPageSize(input.PageSize)
	query := url.Values{"filter": {"parent:accounts/" + input.AccountID}, "pageSize": {strconv.FormatInt(pageSize, 10)}}
	if input.PageToken != "" {
		query.Set("pageToken", input.PageToken)
	}
	response := operation.client.request(call, operation.client.adminBaseURL, http.MethodGet, "/v1beta/properties?"+query.Encode(), nil)
	if decision, failed := classifyRead[PropertiesPage](response, "listProperties", readBranches{rejected: ListPropertiesBranchProviderRejected, invalid: ListPropertiesBranchInvalidResponse, defect: ListPropertiesBranchDefect}); failed {
		return decision
	}
	var page PropertiesPage
	if err := json.Unmarshal(response.body, &page); err != nil || len(page.Properties) > int(pageSize) || len(page.NextPageToken) > 4096 {
		return queryBranch("listProperties", ListPropertiesBranchInvalidResponse, PropertiesPage{}, sdkgo.FailureProtocol, "Google returned invalid property list JSON")
	}
	for _, property := range page.Properties {
		if !strings.HasPrefix(property.Name, "properties/") || property.DisplayName == "" {
			return queryBranch("listProperties", ListPropertiesBranchInvalidResponse, PropertiesPage{}, sdkgo.FailureProtocol, "Google returned incomplete property metadata")
		}
	}
	if page.Properties == nil {
		page.Properties = []Property{}
	}
	return sdkgo.NewQueryBranch(ListPropertiesBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke lists one bounded page of property data streams.
func (operation ListDataStreamsOperation) Invoke(call sdkgo.Call, input ListDataStreamsInput) sdkgo.QueryAttempt[DataStreamsPage] {
	if !validPropertyID(input.PropertyID) || !validAnalyticsPage(input.PageSize, input.PageToken) {
		return queryBranch("listDataStreams", ListDataStreamsBranchDefect, DataStreamsPage{}, sdkgo.FailureValidation, "property or page input is invalid")
	}
	pageSize := analyticsPageSize(input.PageSize)
	query := url.Values{"pageSize": {strconv.FormatInt(pageSize, 10)}}
	if input.PageToken != "" {
		query.Set("pageToken", input.PageToken)
	}
	response := operation.client.request(call, operation.client.adminBaseURL, http.MethodGet, "/v1beta/properties/"+input.PropertyID+"/dataStreams?"+query.Encode(), nil)
	if decision, failed := classifyRead[DataStreamsPage](response, "listDataStreams", readBranches{rejected: ListDataStreamsBranchProviderRejected, invalid: ListDataStreamsBranchInvalidResponse, defect: ListDataStreamsBranchDefect}); failed {
		return decision
	}
	var page DataStreamsPage
	if err := json.Unmarshal(response.body, &page); err != nil || len(page.DataStreams) > int(pageSize) || len(page.NextPageToken) > 4096 {
		return queryBranch("listDataStreams", ListDataStreamsBranchInvalidResponse, DataStreamsPage{}, sdkgo.FailureProtocol, "Google returned invalid data stream JSON")
	}
	for _, stream := range page.DataStreams {
		if !strings.HasPrefix(stream.Name, "properties/"+input.PropertyID+"/dataStreams/") || stream.DisplayName == "" || stream.Type == "" {
			return queryBranch("listDataStreams", ListDataStreamsBranchInvalidResponse, DataStreamsPage{}, sdkgo.FailureProtocol, "Google returned incomplete stream metadata")
		}
	}
	if page.DataStreams == nil {
		page.DataStreams = []DataStream{}
	}
	return sdkgo.NewQueryBranch(ListDataStreamsBranchListed, page, nil, sdkgo.Receipt{})
}

func decodeRealtimeReport(body []byte, dimensionCount, metricCount int, limit int64) (ReportPage, error) {
	var wire struct {
		Rows []struct {
			DimensionValues []struct {
				Value string `json:"value"`
			} `json:"dimensionValues"`
			MetricValues []struct {
				Value string `json:"value"`
			} `json:"metricValues"`
		} `json:"rows"`
		RowCount json.RawMessage `json:"rowCount"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ReportPage{}, err
	}
	if len(wire.Rows) > int(limit) {
		return ReportPage{}, fmt.Errorf("realtime report exceeded row limit")
	}
	rowCount, err := parseNonnegativeCount(wire.RowCount)
	if err != nil {
		return ReportPage{}, err
	}
	page := ReportPage{Rows: make([]ReportRow, 0, len(wire.Rows)), RowCount: rowCount}
	for _, row := range wire.Rows {
		if len(row.DimensionValues) != dimensionCount || len(row.MetricValues) != metricCount {
			return ReportPage{}, fmt.Errorf("realtime report columns do not match request")
		}
		projected := ReportRow{Dimensions: make([]string, 0, dimensionCount), Metrics: make([]string, 0, metricCount)}
		for _, value := range row.DimensionValues {
			projected.Dimensions = append(projected.Dimensions, value.Value)
		}
		for _, value := range row.MetricValues {
			projected.Metrics = append(projected.Metrics, value.Value)
		}
		page.Rows = append(page.Rows, projected)
	}
	return page, nil
}

func validAnalyticsPage(pageSize int64, pageToken string) bool {
	return pageSize >= 0 && pageSize <= 200 && len(pageToken) <= 4096 && !strings.ContainsAny(pageToken, "\r\n")
}

func analyticsPageSize(pageSize int64) int64 {
	if pageSize == 0 {
		return 50
	}
	return pageSize
}
