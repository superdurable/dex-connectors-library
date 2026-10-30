// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package alphavantage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAPIKey = "test-key-123"

var testConnection = sdkgo.ConnectionRef{Provider: "alphavantage", Name: "alphavantage"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
	require.NoError(t, err)
	return client
}

func TestGetGlobalQuoteRequestAndRateLimit(t *testing.T) {
	var rateLimited atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/query", r.URL.Path)
		require.Equal(t, "GLOBAL_QUOTE", r.URL.Query().Get("function"))
		require.Equal(t, "IBM", r.URL.Query().Get("symbol"))
		require.Equal(t, testAPIKey, r.URL.Query().Get("apikey"))
		if rateLimited.Load() {
			_, err := w.Write([]byte(`{"Note":"private rate limit notice"}`))
			require.NoError(t, err)
			return
		}
		_, err := w.Write([]byte(`{"Global Quote":{"01. symbol":"IBM","05. price":"255.25","07. latest trading day":"2026-10-28"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetGlobalQuote(), testConnection, StockInput{Symbol: "IBM"})
	require.NoError(t, err)
	require.Equal(t, GetGlobalQuoteBranchFound, result.Branch)
	require.Equal(t, "255.25", result.Value.Price)
	rateLimited.Store(true)
	_, err = sdkgo.RunQuery(queryContext{}, client.GetGlobalQuote(), testConnection, StockInput{Symbol: "IBM"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), "private rate limit notice")
	require.NotContains(t, err.Error(), testAPIKey)
}

func TestGetTimeSeriesDailyRequestAndErrorClassification(t *testing.T) {
	var rejected atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/query", r.URL.Path)
		require.Equal(t, "TIME_SERIES_DAILY", r.URL.Query().Get("function"))
		require.Equal(t, "compact", r.URL.Query().Get("outputsize"))
		require.Equal(t, testAPIKey, r.URL.Query().Get("apikey"))
		if rejected.Load() {
			_, err := w.Write([]byte(`{"Error Message":"private invalid symbol"}`))
			require.NoError(t, err)
			return
		}
		_, err := w.Write([]byte(`{"Time Series (Daily)":{"2026-10-28":{"1. open":"254.00","2. high":"256.00","3. low":"253.00","4. close":"255.25","5. volume":"1200000"}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetTimeSeriesDaily(), testConnection, StockInput{Symbol: "IBM"})
	require.NoError(t, err)
	require.Equal(t, GetTimeSeriesDailyBranchFound, result.Branch)
	require.Equal(t, "2026-10-28", result.Value.Days[0].Date)
	rejected.Store(true)
	result, err = sdkgo.RunQuery(queryContext{}, client.GetTimeSeriesDaily(), testConnection, StockInput{Symbol: "IBM"})
	require.NoError(t, err)
	require.Equal(t, GetTimeSeriesDailyBranchProviderRejected, result.Branch)
	require.NotContains(t, result.Failure.Message, "private invalid symbol")
	require.NotContains(t, result.Failure.Message, testAPIKey)
}
