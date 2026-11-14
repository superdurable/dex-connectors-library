// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package guance

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

var testConnection = sdkgo.ConnectionRef{Provider: "guance", Name: "guance"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "guance-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-dashboards-page" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func newTestClient(t *testing.T, transport http.RoundTripper, limit int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("api-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestListDashboardsPageRequestAndSafeResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "https://openapi.guance.com/api/v1/dashboards/list?pageIndex=2&pageSize=20", request.URL.String())
		require.Equal(t, "api-secret", request.Header.Get("DF-API-KEY"))
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		return reply(200, `{"code":200,"success":true,"content":[{"uuid":"dsbd_1","name":"Operations","updatorInfo":{"username":"private@guance.com"},"chartPos":[{"secret":"private-chart"}]}],"pageInfo":{"count":1,"pageIndex":2,"pageSize":20,"totalCount":21}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListDashboardsPage(), testConnection, ListDashboardsPageInput{PageIndex: 2, PageSize: 20})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, ListDashboardsPageBranchListed, result.Branch)
	require.Equal(t, ListDashboardsPageOutput{TotalCount: 21, PageIndex: 2, PageSize: 20, Dashboards: []DashboardMetadata{{UUID: "dsbd_1", Name: "Operations"}}}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private@guance.com")
	require.NotContains(t, string(encoded), "private-chart")
}
func TestListDashboardsPageClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"code":200,"success":true,"content":[],"pageInfo":{"count":0,"pageIndex":1,"pageSize":10,"totalCount":0}}`, ListDashboardsPageBranchListed, false},
		{"business failure", 200, `{"code":403,"success":false,"message":"api-secret"}`, ListDashboardsPageBranchProviderRejected, false},
		{"unauthorized", 401, `{"message":"api-secret"}`, ListDashboardsPageBranchProviderRejected, false},
		{"forbidden", 403, `{}`, ListDashboardsPageBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"missing content", 200, `{"code":200,"success":true,"pageInfo":{"count":0,"pageIndex":1,"pageSize":10,"totalCount":0}}`, ListDashboardsPageBranchInvalidResponse, false},
		{"wrong count", 200, `{"code":200,"success":true,"content":[],"pageInfo":{"count":1,"pageIndex":1,"pageSize":10,"totalCount":1}}`, ListDashboardsPageBranchInvalidResponse, false},
		{"missing uuid", 200, `{"code":200,"success":true,"content":[{"name":"A"}],"pageInfo":{"count":1,"pageIndex":1,"pageSize":10,"totalCount":1}}`, ListDashboardsPageBranchInvalidResponse, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(scenario.status, scenario.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListDashboardsPage(), testConnection, ListDashboardsPageInput{})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "api-secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "api-secret")
			}
		})
	}
}
func TestListDashboardsPageValidationAndBounds(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, strings.Repeat("x", 100)), nil })
	client := newTestClient(t, transport, 16)
	for _, input := range []ListDashboardsPageInput{{PageIndex: -1}, {PageSize: 101}, {PageSize: -1}} {
		result, err := sdkgo.RunQuery(queryContext{}, client.ListDashboardsPage(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, ListDashboardsPageBranchDefect, result.Branch)
	}
	require.Zero(t, calls)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListDashboardsPage(), testConnection, ListDashboardsPageInput{})
	require.NoError(t, err)
	require.Equal(t, ListDashboardsPageBranchInvalidResponse, result.Branch)
}
func TestListDashboardsPageTransportAndRedirectAreSecretSafe(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("api-secret") })
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListDashboardsPage(), testConnection, ListDashboardsPageInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "api-secret")
	calls := 0
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		response := reply(302, "")
		response.Header.Set("Location", "https://other.example/steal")
		return response, nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListDashboardsPage(), testConnection, ListDashboardsPageInput{})
	require.NoError(t, err)
	require.Equal(t, ListDashboardsPageBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
}
