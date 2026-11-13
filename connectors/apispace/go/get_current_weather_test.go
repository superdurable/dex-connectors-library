// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package apispace

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

var testConnection = sdkgo.ConnectionRef{Provider: "apispace", Name: "apispace"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "apispace-provider-test" }
func (queryContext) StepExecutionID() string     { return "current-weather" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIToken: sdkgo.NewSecretString("secretTestToken")}})
	require.NoError(t, err)
	return client
}
func TestGetCurrentWeatherRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/456456/weather/v001/now", r.URL.Path)
		require.Equal(t, "101010100", r.URL.Query().Get("areacode"))
		require.Equal(t, "secretTestToken", r.Header.Get("X-APISpace-Token"))
		require.Equal(t, "apikey", r.Header.Get("Authorization-Type"))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		_, err := w.Write([]byte(`{"status":0,"result":{"location":{"name":"北京"},"realtime":{"text":"多云","temp":18.5,"rh":38},"last_update":"2026-11-13 12:00:00"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetCurrentWeather(), testConnection, GetCurrentWeatherInput{AreaCode: "101010100"})
	require.NoError(t, err)
	require.Equal(t, GetCurrentWeatherBranchFound, result.Branch)
	require.Equal(t, CurrentWeather{City: "北京", Condition: "多云", TemperatureCelsius: 18.5, HumidityPercent: 38, UpdatedAtBeijing: "2026-11-13 12:00:00"}, result.Value)
	require.EqualValues(t, 1, calls.Load())
}
func TestGetCurrentWeatherOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"provider rejection", 200, `{"status":1}`, GetCurrentWeatherBranchProviderRejected, false},
		{"invalid token", 401, `{}`, GetCurrentWeatherBranchProviderRejected, false},
		{"plan exhausted", 416, `{}`, GetCurrentWeatherBranchProviderRejected, false},
		{"rate limit", 413, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"malformed", 200, `{`, GetCurrentWeatherBranchInvalidResponse, false},
		{"missing result", 200, `{"status":0}`, GetCurrentWeatherBranchInvalidResponse, false},
		{"missing field", 200, `{"status":0,"result":{"location":{"name":"北京"},"realtime":{"text":"多云","temp":18.5},"last_update":"now"}}`, GetCurrentWeatherBranchInvalidResponse, false},
		{"bad humidity", 200, `{"status":0,"result":{"location":{"name":"北京"},"realtime":{"text":"多云","temp":18.5,"rh":101},"last_update":"now"}}`, GetCurrentWeatherBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetCurrentWeather(), testConnection, GetCurrentWeatherInput{AreaCode: "101010100"})
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
			}
		})
	}
}
func TestInvalidInputAndSecretNeverCallProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetCurrentWeather(), testConnection, GetCurrentWeatherInput{AreaCode: "../bad"})
	require.NoError(t, err)
	require.Equal(t, GetCurrentWeatherBranchDefect, result.Branch)
	invalid, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIToken: sdkgo.NewSecretString("bad token")}})
	require.NoError(t, err)
	result, err = sdkgo.RunQuery(queryContext{}, invalid.GetCurrentWeather(), testConnection, GetCurrentWeatherInput{AreaCode: "101010100"})
	require.NoError(t, err)
	require.Equal(t, GetCurrentWeatherBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
func TestRedirectDoesNotForwardSecret(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetCurrentWeather(), testConnection, GetCurrentWeatherInput{AreaCode: "101010100"})
	require.NoError(t, err)
	require.Equal(t, GetCurrentWeatherBranchProviderRejected, result.Branch)
	require.False(t, forwarded.Load())
	require.NotContains(t, result.Failure.Message, "secretTestToken")
	require.False(t, strings.Contains(result.Failure.Message, "X-APISpace-Token"))
}
