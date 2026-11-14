// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package seniverse

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testPrivateKey = "test-seniverse-private-key"

var testConnection = sdkgo.ConnectionRef{Provider: "seniverse", Name: "seniverse"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "seniverse-provider-test" }
func (queryContext) StepExecutionID() string     { return "current-weather" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {PrivateKey: sdkgo.NewSecretString(testPrivateKey)}})
	require.NoError(t, err)
	return client
}

func TestCurrentWeatherRequestAndFreePlanResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v3/weather/now.json", request.URL.Path)
		for key, want := range map[string]string{"key": testPrivateKey, "location": "北京", "language": "zh-Hans", "unit": "c"} {
			require.Equal(t, want, request.URL.Query().Get(key), key)
		}
		_, err := writer.Write([]byte(`{"results":[{"location":{"id":"WX4FBXXFKE4F","name":"北京","country":"CN"},"now":{"text":"晴","code":"0","temperature":"20"},"last_update":"2026-01-01T12:00:00+08:00"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetCurrentWeather(), testConnection, GetCurrentWeatherInput{Location: "北京"})
	require.NoError(t, err)
	require.Equal(t, GetCurrentWeatherBranchFetched, result.Branch)
	require.Equal(t, "北京", result.Value.LocationName)
	require.Equal(t, "晴", result.Value.WeatherText)
	require.Equal(t, "20", result.Value.Temperature)
	require.Equal(t, "c", result.Value.TemperatureUnit)
	require.EqualValues(t, 1, calls.Load())
}

func TestCurrentWeatherOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"invalid key", 403, `{"status":"invalid","status_code":"AP010003"}`, GetCurrentWeatherBranchProviderRejected, false},
		{"unknown location", 404, `{"status":"not found","status_code":"AP010010"}`, GetCurrentWeatherBranchProviderRejected, false},
		{"quota exhausted", 403, `{"status":"quota","status_code":"AP010013"}`, GetCurrentWeatherBranchProviderRejected, false},
		{"provider rate limit", 403, `{"status":"rate","status_code":"AP010014"}`, "", true},
		{"http rate limit", 429, `private provider data`, "", true},
		{"server failure", 503, `private provider data`, "", true},
		{"malformed", 200, `{`, GetCurrentWeatherBranchInvalidResponse, false},
		{"missing results", 200, `{"results":[]}`, GetCurrentWeatherBranchInvalidResponse, false},
		{"missing free field", 200, `{"results":[{"location":{"id":"id","name":"北京"},"now":{"text":"晴","code":"0"},"last_update":"2026-01-01T12:00:00+08:00"}]}`, GetCurrentWeatherBranchInvalidResponse, false},
		{"invalid temperature", 200, `{"results":[{"location":{"id":"id","name":"北京"},"now":{"text":"晴","code":"0","temperature":"NaN"},"last_update":"2026-01-01T12:00:00+08:00"}]}`, GetCurrentWeatherBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(tc.status)
				_, err := writer.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetCurrentWeather(), testConnection, GetCurrentWeatherInput{Location: "北京"})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testPrivateKey)
				require.NotContains(t, err.Error(), "private provider data")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}

func TestInvalidLocationSendsNoRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { calls.Add(1); writer.WriteHeader(http.StatusOK) }))
	defer server.Close()
	for _, location := range []string{"", " 北京", strings.Repeat("x", 257)} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetCurrentWeather(), testConnection, GetCurrentWeatherInput{Location: location})
		require.NoError(t, err)
		require.Equal(t, GetCurrentWeatherBranchDefect, result.Branch)
	}
	require.Zero(t, calls.Load())
}

func TestOversizedResponseAndTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"results":[{"location":{"id":"id","name":"北京"},"now":{"text":"晴","code":"0","temperature":"20"},"last_update":"2026-01-01T12:00:00+08:00"}]}`))
		require.NoError(t, err)
	}))
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {PrivateKey: sdkgo.NewSecretString(testPrivateKey)}}
	client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 8}, credentials)
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetCurrentWeather(), testConnection, GetCurrentWeatherInput{Location: "北京"})
	require.NoError(t, err)
	require.Equal(t, GetCurrentWeatherBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetCurrentWeather(), testConnection, GetCurrentWeatherInput{Location: "北京"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.NotContains(t, err.Error(), testPrivateKey)
}
