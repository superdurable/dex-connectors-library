// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package datadog

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type testContext struct{ dex.Context }

func (testContext) FlowID() string              { return "provider-test" }
func (testContext) StepExecutionID() string     { return "test" }
func (testContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (testContext) Done() <-chan struct{}       { return nil }
func (testContext) Err() error                  { return nil }
func (testContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "datadog", Name: "datadog"}

func testClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("test-apikey"), ApplicationKey: sdkgo.NewSecretString("test-applicationkey")}})
	require.NoError(t, err)
	return client
}

func TestQueryMetricsProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "GET", r.Method)
		require.Equal(t, "/api/v1/query", r.URL.EscapedPath())
		require.Equal(t, "test-apikey", r.Header.Get("DD-API-KEY"))
		require.Equal(t, "test-applicationkey", r.Header.Get("DD-APPLICATION-KEY"))
		require.NotContains(t, r.URL.RawQuery, "test-")
		_, err := fmt.Fprint(w, `{"status":"ok","series":[{"metric":"system.cpu.user","pointlist":[[1761436800000,0.23]]}]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	result, err := sdkgo.RunQuery(testContext{}, client.QueryMetrics(), testConnection, QueryMetricsInput{From: 1761436800, To: 1761440400, Query: "system.cpu.user{*}"})
	require.NoError(t, err)
	require.Equal(t, QueryMetricsBranchQueried, result.Branch)
	require.Nil(t, result.Failure)
}
func TestQueryMetricsRetriesRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	defer server.Close()
	client := testClient(t, server.URL)
	_, err := sdkgo.RunQuery(testContext{}, client.QueryMetrics(), testConnection, QueryMetricsInput{From: 1761436800, To: 1761440400, Query: "system.cpu.user{*}"})
	require.Error(t, err)
	require.Contains(t, err.Error(), string(sdkgo.FailureRateLimit))
}

func TestListMonitorsProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "GET", r.Method)
		require.Equal(t, "/api/v1/monitor", r.URL.EscapedPath())
		require.Equal(t, "test-apikey", r.Header.Get("DD-API-KEY"))
		require.Equal(t, "test-applicationkey", r.Header.Get("DD-APPLICATION-KEY"))
		require.NotContains(t, r.URL.RawQuery, "test-")
		_, err := fmt.Fprint(w, `[{"id":12345,"name":"Meridian API latency","type":"metric alert","overall_state":"OK"}]`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	result, err := sdkgo.RunQuery(testContext{}, client.ListMonitors(), testConnection, ListMonitorsInput{Page: 0, PageSize: 1})
	require.NoError(t, err)
	require.Equal(t, ListMonitorsBranchListed, result.Branch)
	require.Nil(t, result.Failure)
}
func TestListMonitorsRetriesRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	defer server.Close()
	client := testClient(t, server.URL)
	_, err := sdkgo.RunQuery(testContext{}, client.ListMonitors(), testConnection, ListMonitorsInput{Page: 0, PageSize: 1})
	require.Error(t, err)
	require.Contains(t, err.Error(), string(sdkgo.FailureRateLimit))
}
