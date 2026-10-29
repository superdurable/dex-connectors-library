// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openweathermap

import (
	"context"
	"io"
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

var testConnection = sdkgo.ConnectionRef{Provider: "openweathermap", Name: "openweathermap"}

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

func TestGetCurrentWeatherProviderRequestAndRateLimit(t *testing.T) {
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/data/2.5/weather", r.URL.Path)
		require.Equal(t, "GET", r.Method)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		_ = body
		require.Equal(t, testAPIKey, r.URL.Query().Get("appid"))
		require.Equal(t, "metric", r.URL.Query().Get("units"))
		if status.Load() == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, err = w.Write([]byte("private provider text"))
			require.NoError(t, err)
			return
		}
		_, err = w.Write([]byte(`{"name":"San Francisco","main":{"temp":19.5},"weather":[{"description":"clear sky"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetCurrentWeather(), testConnection, WeatherInput{Latitude: 37.7, Longitude: -122.4, Units: "metric"})
	require.NoError(t, err)
	require.Equal(t, GetCurrentWeatherBranchFound, result.Branch)
	require.Equal(t, 19.5, result.Value.Temperature)
	status.Store(1)
	_, err = sdkgo.RunQuery(queryContext{}, client.GetCurrentWeather(), testConnection, WeatherInput{Latitude: 37.7, Longitude: -122.4, Units: "metric"})
	var retry *dex.RetryAfterError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, 2*time.Second, retry.After)
	require.NotContains(t, err.Error(), "private provider text")
	require.NotContains(t, err.Error(), testAPIKey)
}

func TestGetForecastProviderRequestAndRateLimit(t *testing.T) {
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/data/2.5/forecast", r.URL.Path)
		require.Equal(t, "GET", r.Method)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		_ = body
		require.Equal(t, testAPIKey, r.URL.Query().Get("appid"))
		require.Equal(t, "37.7", r.URL.Query().Get("lat"))
		if status.Load() == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, err = w.Write([]byte("private provider text"))
			require.NoError(t, err)
			return
		}
		_, err = w.Write([]byte(`{"city":{"name":"San Francisco"},"list":[{"dt":1793100000,"main":{"temp":18.2}}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetForecast(), testConnection, WeatherInput{Latitude: 37.7, Longitude: -122.4, Units: "metric"})
	require.NoError(t, err)
	require.Equal(t, GetForecastBranchFound, result.Branch)
	require.Len(t, result.Value.Entries, 1)
	status.Store(1)
	_, err = sdkgo.RunQuery(queryContext{}, client.GetForecast(), testConnection, WeatherInput{Latitude: 37.7, Longitude: -122.4, Units: "metric"})
	var retry *dex.RetryAfterError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, 2*time.Second, retry.After)
	require.NotContains(t, err.Error(), "private provider text")
	require.NotContains(t, err.Error(), testAPIKey)
}
