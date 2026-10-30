// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package datadog exposes typed Dex Steps for the Datadog REST API.
package datadog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies an HTTP transport without serializing it into a Dex Flow.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a client transport; New copies it and disables redirects.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable HTTP settings and resolves secrets per request.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates configuration without contacting Datadog.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Datadog credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Datadog base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

type requestSpec struct {
	method, path, contentType string
	query                     url.Values
	body                      []byte
}

// QueryMetricsInput supplies the metric expression and Unix-second window.
type QueryMetricsInput struct {
	// From is the inclusive start in Unix seconds.
	From int64 `json:"from"`
	// To is the exclusive end in Unix seconds and must follow From.
	To int64 `json:"to"`
	// Query is a Datadog metric query expression.
	Query string `json:"query"`
}

// MetricSeries is one Datadog timeseries.
type MetricSeries struct {
	// Metric is the queried metric name.
	Metric string `json:"metric"`
	// Pointlist holds pairs of millisecond timestamps and possibly null values.
	Pointlist [][]*float64 `json:"pointlist"`
}

// MetricsQuery is Datadog's metric query result.
type MetricsQuery struct {
	// Status is Datadog's query status, normally ok.
	Status string `json:"status"`
	// Series contains the matching timeseries.
	Series []MetricSeries `json:"series"`
}

// ListMonitorsInput selects a zero-based monitor page.
type ListMonitorsInput struct {
	// Page is zero-based; zero selects the first page.
	Page int `json:"page"`
	// PageSize is the number of monitors requested; zero selects 100.
	PageSize int `json:"pageSize"`
}

// Monitor is a Datadog monitor summary.
type Monitor struct {
	// ID is the monitor's numeric identifier.
	ID int64 `json:"id"`
	// Name is the monitor's display name.
	Name string `json:"name"`
	// Type is the monitor kind, such as metric alert.
	Type string `json:"type"`
	// OverallState is the provider-reported alert state.
	OverallState string `json:"overall_state"`
}

// MonitorsPage contains the returned monitor page.
type MonitorsPage struct {
	// Monitors contains the requested monitor page, possibly empty.
	Monitors []Monitor `json:"monitors"`
}

// QueryMetricsOperation invokes queryMetrics with one provider request.
type QueryMetricsOperation struct{ client *Client }

// QueryMetrics binds the operation to this client.
func (client *Client) QueryMetrics() QueryMetricsOperation { return QueryMetricsOperation{client} }

// Definition returns the generated queryMetrics contract.
func (QueryMetricsOperation) Definition() sdkgo.QueryDefinition { return QueryMetricsDefinition }

// Invoke validates input, calls Datadog, and classifies the response.
func (operation QueryMetricsOperation) Invoke(call sdkgo.Call, input QueryMetricsInput) sdkgo.QueryAttempt[MetricsQuery] {
	spec, err := buildQueryMetricsRequest(input)
	if err != nil {
		return sdkgo.NewQueryBranch(QueryMetricsBranchDefect, MetricsQuery{}, failure(sdkgo.FailureValidation, "queryMetrics", err.Error()), sdkgo.Receipt{})
	}
	return executeQuery(operation.client, call, "queryMetrics", spec, QueryMetricsBranchQueried, "", QueryMetricsBranchProviderRejected, QueryMetricsBranchInvalidResponse, QueryMetricsBranchDefect, decodeQueryMetrics)
}
func buildQueryMetricsRequest(input QueryMetricsInput) (requestSpec, error) {
	query := url.Values{"from": {strconv.FormatInt(input.From, 10)}, "to": {strconv.FormatInt(input.To, 10)}, "query": {input.Query}}
	if input.From <= 0 || input.To <= input.From || strings.TrimSpace(input.Query) == "" {
		return requestSpec{}, fmt.Errorf("from, to, and query are required; to must follow from")
	}
	return requestSpec{method: http.MethodGet, path: "/api/v1/query", query: query}, nil
}
func decodeQueryMetrics(body []byte) (MetricsQuery, error) {
	var value MetricsQuery
	if err := json.Unmarshal(body, &value); err != nil {
		return MetricsQuery{}, err
	}
	if value.Status != "ok" || value.Series == nil {
		return MetricsQuery{}, fmt.Errorf("missing successful metric series")
	}
	return value, nil
}

// ListMonitorsOperation invokes listMonitors with one provider request.
type ListMonitorsOperation struct{ client *Client }

// ListMonitors binds the operation to this client.
func (client *Client) ListMonitors() ListMonitorsOperation { return ListMonitorsOperation{client} }

// Definition returns the generated listMonitors contract.
func (ListMonitorsOperation) Definition() sdkgo.QueryDefinition { return ListMonitorsDefinition }

// Invoke validates input, calls Datadog, and classifies the response.
func (operation ListMonitorsOperation) Invoke(call sdkgo.Call, input ListMonitorsInput) sdkgo.QueryAttempt[MonitorsPage] {
	spec, err := buildListMonitorsRequest(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListMonitorsBranchDefect, MonitorsPage{}, failure(sdkgo.FailureValidation, "listMonitors", err.Error()), sdkgo.Receipt{})
	}
	return executeQuery(operation.client, call, "listMonitors", spec, ListMonitorsBranchListed, "", ListMonitorsBranchProviderRejected, ListMonitorsBranchInvalidResponse, ListMonitorsBranchDefect, decodeListMonitors)
}
func buildListMonitorsRequest(input ListMonitorsInput) (requestSpec, error) {
	if input.Page < 0 || input.PageSize < 0 || input.PageSize > 1000 {
		return requestSpec{}, fmt.Errorf("page must be nonnegative and pageSize at most 1000")
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 100
	}
	query := url.Values{"page": {strconv.Itoa(input.Page)}, "page_size": {strconv.Itoa(pageSize)}}
	return requestSpec{method: http.MethodGet, path: "/api/v1/monitor", query: query}, nil
}
func decodeListMonitors(body []byte) (MonitorsPage, error) {
	var items []Monitor
	if err := json.Unmarshal(body, &items); err != nil {
		return MonitorsPage{}, err
	}
	if items == nil {
		return MonitorsPage{}, fmt.Errorf("missing monitors")
	}
	return MonitorsPage{Monitors: items}, nil
}

func executeQuery[T any](client *Client, call sdkgo.Call, operation string, spec requestSpec, happy, notFound, rejected, invalid, defect sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	var zero T
	response, localFailure, err := client.send(call, operation, spec)
	if localFailure != nil {
		localFailure.Operation = operation
		return sdkgo.NewQueryBranch(defect, zero, localFailure, sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "datadog", Operation: operation, Message: "Datadog request failed"}, 0)
	}
	// Closing the response cannot change its classified provider outcome.
	defer func() { _ = response.Body.Close() }()
	if shouldRetry(response) {
		retry, after := retryFailure(response, operation)
		return sdkgo.NewQueryRetry[T](retry, after)
	}
	if response.StatusCode == http.StatusNotFound && notFound != "" {
		rejectedFailure := statusFailure(response, operation)
		return sdkgo.NewQueryBranch(notFound, zero, &rejectedFailure, sdkgo.Receipt{})
	}
	if response.StatusCode != http.StatusOK {
		rejectedFailure := statusFailure(response, operation)
		return sdkgo.NewQueryBranch(rejected, zero, &rejectedFailure, sdkgo.Receipt{})
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		if errors.Is(err, providerhttp.ErrBodyTooLarge) {
			return sdkgo.NewQueryBranch(invalid, zero, failure(sdkgo.FailureResponseTooLarge, operation, "response exceeded the configured limit"), sdkgo.Receipt{})
		}
		return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "datadog", Operation: operation, Message: "response was interrupted"}, 0)
	}
	value, err := decode(body)
	if err != nil {
		return sdkgo.NewQueryBranch(invalid, zero, failure(sdkgo.FailureProtocol, operation, "response was malformed"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(happy, value, nil, sdkgo.Receipt{})
}

func (client *Client) send(call sdkgo.Call, operation string, spec requestSpec) (*http.Response, *sdkgo.Failure, error) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil {
		return nil, failure(sdkgo.FailureAuthentication, operation, "connection credentials are unavailable"), nil
	}
	if err := credentials.Validate(); err != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) || !providerhttp.IsHeaderSafeCredential(credentials.ApplicationKey.Reveal()) {
		return nil, failure(sdkgo.FailureAuthentication, operation, "connection credentials are invalid"), nil
	}
	endpoint := client.baseURL + spec.path
	if len(spec.query) != 0 {
		endpoint += "?" + spec.query.Encode()
	}
	request, err := http.NewRequestWithContext(call.Context, spec.method, endpoint, bytes.NewReader(spec.body))
	if err != nil {
		return nil, failure(sdkgo.FailureLocalDefect, operation, "request could not be built"), nil
	}
	request.Header.Set("DD-API-KEY", credentials.APIKey.Reveal())
	request.Header.Set("DD-APPLICATION-KEY", credentials.ApplicationKey.Reveal())
	request.Header.Set("Accept", "application/json")
	if spec.contentType != "" {
		request.Header.Set("Content-Type", spec.contentType)
	}
	response, err := client.httpClient.Do(request)
	return response, nil, err
}

func statusFailure(response *http.Response, operation string) sdkgo.Failure {
	kind := sdkgo.FailureProviderRejection
	switch response.StatusCode {
	case http.StatusUnauthorized:
		kind = sdkgo.FailureAuthentication
	case http.StatusForbidden:
		kind = sdkgo.FailureAuthorization
	case http.StatusNotFound:
		kind = sdkgo.FailureNotFound
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		kind = sdkgo.FailureValidation
	}
	return sdkgo.Failure{Kind: kind, Provider: "datadog", Operation: operation, Message: fmt.Sprintf("Datadog returned HTTP %d", response.StatusCode)}
}

func shouldRetry(response *http.Response) bool {
	return response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict || response.StatusCode >= http.StatusInternalServerError
}

func retryFailure(response *http.Response, operation string) (sdkgo.Failure, time.Duration) {
	kind := sdkgo.FailureAvailability
	if response.StatusCode == http.StatusTooManyRequests {
		kind = sdkgo.FailureRateLimit
	}
	return sdkgo.Failure{Kind: kind, Provider: "datadog", Operation: operation, Message: fmt.Sprintf("Datadog returned HTTP %d", response.StatusCode)}, providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
}

func failure(kind sdkgo.FailureKind, operation, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: "datadog", Operation: operation, Message: message}
}
