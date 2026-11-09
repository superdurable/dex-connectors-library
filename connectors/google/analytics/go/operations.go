// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package analytics4

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var analyticsName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_:]{0,127}$`)
var propertyID = regexp.MustCompile(`^[0-9]{1,30}$`)

// RunReportInput selects one GA4 report page with up to nine dimensions and ten metrics.
type RunReportInput struct {
	// PropertyID is the numeric GA4 Property ID from Analytics Admin.
	PropertyID string `json:"propertyId"`
	// StartDate is the inclusive date in YYYY-MM-DD form.
	StartDate string `json:"startDate"`
	// EndDate is the inclusive date in YYYY-MM-DD form.
	EndDate string `json:"endDate"`
	// Dimensions are up to nine GA4 Data API dimension names in requested order.
	Dimensions []string `json:"dimensions"`
	// Metrics are one to ten GA4 Data API metric names in requested order.
	Metrics []string `json:"metrics"`
	// Limit is the page size from 1 to 10000; zero uses 100.
	Limit int64 `json:"limit"`
	// Offset is the zero-based row offset.
	Offset int64 `json:"offset"`
}

// ReportRow contains ordered dimension and metric strings from one GA4 row.
type ReportRow struct {
	// Dimensions match the input dimension order.
	Dimensions []string `json:"dimensions"`
	// Metrics match the input metric order.
	Metrics []string `json:"metrics"`
}

// ReportPage is a bounded GA4 report page and the total matching row count.
type ReportPage struct {
	// Rows are at most the requested limit.
	Rows []ReportRow `json:"rows"`
	// RowCount is the total number of matching rows across all pages.
	RowCount int64 `json:"rowCount"`
}

// GetPropertyInput selects one numeric GA4 property ID.
type GetPropertyInput struct {
	// PropertyID is the numeric GA4 Property ID from Analytics Admin.
	PropertyID string `json:"propertyId"`
}

// Property contains non-secret GA4 property metadata.
type Property struct {
	// Name is the GA4 resource name, such as properties/123456789.
	Name string `json:"name"`
	// DisplayName is the human-readable name in Analytics Admin.
	DisplayName string `json:"displayName"`
	// TimeZone is the IANA reporting time zone.
	TimeZone string `json:"timeZone"`
	// CurrencyCode is the property's ISO 4217 reporting currency when configured.
	CurrencyCode string `json:"currencyCode"`
}

// ListAudiencesInput selects one page of audiences for a numeric property ID.
type ListAudiencesInput struct {
	// PropertyID is the numeric GA4 Property ID from Analytics Admin.
	PropertyID string `json:"propertyId"`
	// PageSize is from 1 to 200; zero uses 50.
	PageSize int64 `json:"pageSize"`
	// PageToken is Google's opaque cursor from a prior page.
	PageToken string `json:"pageToken"`
}

// Audience contains non-secret identity and display metadata.
type Audience struct {
	// Name is the GA4 audience resource name.
	Name string `json:"name"`
	// DisplayName is the name visible in Analytics Admin.
	DisplayName string `json:"displayName"`
	// Description is the audience description, which may be blank.
	Description string `json:"description"`
}

// AudiencesPage contains one bounded audience page and optional next token.
type AudiencesPage struct {
	// Audiences are the audience identities on this page.
	Audiences []Audience `json:"audiences"`
	// NextPageToken is Google's opaque cursor; blank means the page is final.
	NextPageToken string `json:"nextPageToken"`
}

// RunReportOperation reads one GA4 Data API report page.
type RunReportOperation struct{ client *Client }

// GetPropertyOperation reads one GA4 property.
type GetPropertyOperation struct{ client *Client }

// ListAudiencesOperation reads one GA4 audience page.
type ListAudiencesOperation struct{ client *Client }

// RunReport binds the report Query.
func (client *Client) RunReport() RunReportOperation { return RunReportOperation{client} }

// GetProperty binds the property Query.
func (client *Client) GetProperty() GetPropertyOperation { return GetPropertyOperation{client} }

// ListAudiences binds the audience Query.
func (client *Client) ListAudiences() ListAudiencesOperation { return ListAudiencesOperation{client} }

// Definition returns the generated report Query contract.
func (RunReportOperation) Definition() sdkgo.QueryDefinition { return RunReportDefinition }

// Definition returns the generated property Query contract.
func (GetPropertyOperation) Definition() sdkgo.QueryDefinition { return GetPropertyDefinition }

// Definition returns the generated audience Query contract.
func (ListAudiencesOperation) Definition() sdkgo.QueryDefinition { return ListAudiencesDefinition }

// Invoke sends a bounded report request; POST is a read-only Google Data API method.
func (operation RunReportOperation) Invoke(call sdkgo.Call, input RunReportInput) sdkgo.QueryAttempt[ReportPage] {
	if !validPropertyID(input.PropertyID) || !validDateRange(input.StartDate, input.EndDate) || !validNames(input.Dimensions, maxReportDimensions, false) || !validNames(input.Metrics, maxReportMetrics, true) || input.Limit < 0 || input.Limit > 10000 || input.Offset < 0 || input.Offset > 1000000000 {
		return queryBranch("runReport", RunReportBranchDefect, ReportPage{}, sdkgo.FailureValidation, "report input is invalid")
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
	requestBody, err := json.Marshal(struct {
		DateRanges []map[string]string `json:"dateRanges"`
		Dimensions []map[string]string `json:"dimensions"`
		Metrics    []map[string]string `json:"metrics"`
		Limit      string              `json:"limit"`
		Offset     string              `json:"offset"`
	}{[]map[string]string{{"startDate": input.StartDate, "endDate": input.EndDate}}, dimensions, metrics, strconv.FormatInt(limit, 10), strconv.FormatInt(input.Offset, 10)})
	if err != nil {
		return queryBranch("runReport", RunReportBranchDefect, ReportPage{}, sdkgo.FailureValidation, "report request could not be encoded")
	}
	response := operation.client.request(call, operation.client.dataBaseURL, http.MethodPost, "/v1beta/properties/"+input.PropertyID+":runReport", requestBody)
	if decision, failed := classifyRead[ReportPage](response, "runReport", readBranches{rejected: RunReportBranchProviderRejected, invalid: RunReportBranchInvalidResponse, defect: RunReportBranchDefect}); failed {
		return decision
	}
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
	if err := json.Unmarshal(response.body, &wire); err != nil || len(wire.Rows) > int(limit) || len(wire.Rows) > 10000 {
		return queryBranch("runReport", RunReportBranchInvalidResponse, ReportPage{}, sdkgo.FailureProtocol, "Google returned invalid report JSON")
	}
	rowCount, err := parseNonnegativeCount(wire.RowCount)
	if err != nil {
		return queryBranch("runReport", RunReportBranchInvalidResponse, ReportPage{}, sdkgo.FailureProtocol, "Google returned invalid report row count")
	}
	page := ReportPage{Rows: make([]ReportRow, 0, len(wire.Rows)), RowCount: rowCount}
	for _, row := range wire.Rows {
		if len(row.DimensionValues) != len(input.Dimensions) || len(row.MetricValues) != len(input.Metrics) {
			return queryBranch("runReport", RunReportBranchInvalidResponse, ReportPage{}, sdkgo.FailureProtocol, "Google returned mismatched report columns")
		}
		projected := ReportRow{Dimensions: make([]string, 0, len(row.DimensionValues)), Metrics: make([]string, 0, len(row.MetricValues))}
		for _, value := range row.DimensionValues {
			projected.Dimensions = append(projected.Dimensions, value.Value)
		}
		for _, value := range row.MetricValues {
			projected.Metrics = append(projected.Metrics, value.Value)
		}
		page.Rows = append(page.Rows, projected)
	}
	return sdkgo.NewQueryBranch(RunReportBranchReported, page, nil, sdkgo.Receipt{})
}

// Invoke reads the display metadata for one GA4 property.
func (operation GetPropertyOperation) Invoke(call sdkgo.Call, input GetPropertyInput) sdkgo.QueryAttempt[Property] {
	if !validPropertyID(input.PropertyID) {
		return queryBranch("getProperty", GetPropertyBranchDefect, Property{}, sdkgo.FailureValidation, "property ID must be numeric")
	}
	response := operation.client.request(call, operation.client.adminBaseURL, http.MethodGet, "/v1beta/properties/"+input.PropertyID, nil)
	if decision, failed := classifyRead[Property](response, "getProperty", readBranches{notFound: GetPropertyBranchNotFound, rejected: GetPropertyBranchProviderRejected, invalid: GetPropertyBranchInvalidResponse, defect: GetPropertyBranchDefect}); failed {
		return decision
	}
	var property Property
	if err := json.Unmarshal(response.body, &property); err != nil || property.Name != "properties/"+input.PropertyID || property.DisplayName == "" || property.TimeZone == "" {
		return queryBranch("getProperty", GetPropertyBranchInvalidResponse, Property{}, sdkgo.FailureProtocol, "Google returned invalid property JSON")
	}
	return sdkgo.NewQueryBranch(GetPropertyBranchFound, property, nil, sdkgo.Receipt{})
}

// Invoke reads one audience page, projecting identity and display fields.
func (operation ListAudiencesOperation) Invoke(call sdkgo.Call, input ListAudiencesInput) sdkgo.QueryAttempt[AudiencesPage] {
	if !validPropertyID(input.PropertyID) || input.PageSize < 0 || input.PageSize > 200 || len(input.PageToken) > 4096 || strings.ContainsAny(input.PageToken, "\r\n") {
		return queryBranch("listAudiences", ListAudiencesBranchDefect, AudiencesPage{}, sdkgo.FailureValidation, "audience page input is invalid")
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 50
	}
	query := url.Values{"pageSize": []string{strconv.FormatInt(pageSize, 10)}}
	if input.PageToken != "" {
		query.Set("pageToken", input.PageToken)
	}
	response := operation.client.request(call, operation.client.adminBaseURL, http.MethodGet, "/v1alpha/properties/"+input.PropertyID+"/audiences?"+query.Encode(), nil)
	if decision, failed := classifyRead[AudiencesPage](response, "listAudiences", readBranches{rejected: ListAudiencesBranchProviderRejected, invalid: ListAudiencesBranchInvalidResponse, defect: ListAudiencesBranchDefect}); failed {
		return decision
	}
	var wire struct {
		Audiences     []Audience `json:"audiences"`
		NextPageToken string     `json:"nextPageToken"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || len(wire.Audiences) > int(pageSize) || len(wire.NextPageToken) > 4096 {
		return queryBranch("listAudiences", ListAudiencesBranchInvalidResponse, AudiencesPage{}, sdkgo.FailureProtocol, "Google returned invalid audience JSON")
	}
	page := AudiencesPage{Audiences: make([]Audience, 0, len(wire.Audiences)), NextPageToken: wire.NextPageToken}
	for _, audience := range wire.Audiences {
		if !strings.HasPrefix(audience.Name, "properties/"+input.PropertyID+"/audiences/") || audience.DisplayName == "" {
			return queryBranch("listAudiences", ListAudiencesBranchInvalidResponse, AudiencesPage{}, sdkgo.FailureProtocol, "Google returned incomplete audience metadata")
		}
		page.Audiences = append(page.Audiences, audience)
	}
	return sdkgo.NewQueryBranch(ListAudiencesBranchListed, page, nil, sdkgo.Receipt{})
}

type readBranches struct{ notFound, rejected, invalid, defect sdkgo.BranchID }

func classifyRead[T any](response providerResponse, operation string, branches readBranches) (sdkgo.QueryAttempt[T], bool) {
	var zero T
	if errors.Is(response.err, errCredential) {
		return queryBranch(operation, branches.defect, zero, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid"), true
	}
	if errors.Is(response.err, errRequest) {
		return queryRetry[T](operation, sdkgo.FailureTransport, "Google Analytics request failed", 0), true
	}
	if response.status == 429 {
		return queryRetry[T](operation, sdkgo.FailureRateLimit, "Google rate limited the request", response.retryAfter), true
	}
	if response.status == 408 || response.status >= 500 {
		return queryRetry[T](operation, sdkgo.FailureAvailability, "Google Analytics is temporarily unavailable", response.retryAfter), true
	}
	if response.status == 404 && branches.notFound != "" {
		return queryBranch(operation, branches.notFound, zero, sdkgo.FailureNotFound, "Google did not find the property"), true
	}
	if response.status != 200 {
		return queryBranch(operation, branches.rejected, zero, sdkgo.FailureProviderRejection, "Google rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return queryBranch(operation, branches.invalid, zero, sdkgo.FailureResponseTooLarge, "Google response exceeded the configured limit"), true
	}
	if response.err != nil {
		return queryRetry[T](operation, sdkgo.FailureTransport, "Google response was interrupted", 0), true
	}
	return sdkgo.QueryAttempt[T]{}, false
}

func queryBranch[T any](operation string, branch sdkgo.BranchID, value T, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-analytics-4", Operation: operation, Message: message}
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}

func queryRetry[T any](operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-analytics-4", Operation: operation, Message: message}
	return sdkgo.NewQueryRetry[T](failure, delay)
}

func validPropertyID(value string) bool { return propertyID.MatchString(value) }

func validDateRange(start, end string) bool {
	first, err := time.Parse("2006-01-02", start)
	if err != nil || first.Format("2006-01-02") != start {
		return false
	}
	last, err := time.Parse("2006-01-02", end)
	return err == nil && last.Format("2006-01-02") == end && !last.Before(first)
}

// GA4 runReport accepts at most nine dimensions and ten metrics.
const (
	maxReportDimensions = 9
	maxReportMetrics    = 10
)

func validNames(names []string, maxCount int, required bool) bool {
	if len(names) > maxCount || required && len(names) == 0 {
		return false
	}
	for _, name := range names {
		if !analyticsName.MatchString(name) {
			return false
		}
	}
	return true
}

func parseNonnegativeCount(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 {
		return 0, nil
	}
	text := strings.Trim(string(raw), `"`)
	count, err := strconv.ParseInt(text, 10, 64)
	if err != nil || count < 0 {
		return 0, errors.New("invalid row count")
	}
	return count, nil
}
