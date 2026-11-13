// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qweather

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "qweather", Name: "qweather"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "qweather-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-current-weather" }
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
func newTestClient(t *testing.T, transport http.RoundTripper, host string, limit int64) *Client {
	t.Helper()
	credentials := Credentials{APIHost: sdkgo.NewSecretString(host), APIKey: sdkgo.NewSecretString("secret-api-key")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}

var beijing = GetCurrentWeatherInput{Latitude: 39.92, Longitude: 116.41}

const weatherJSON = `{"metadata":{"attributions":["https://developer.qweather.com/attribution.html"]},"validTime":"2024-05-31T03:15Z","condition":{"text":"Few Clouds","code":"102"},"temperature":{"value":31.71,"unit":"°C"},"humidity":0.69}`

func TestGetCurrentWeatherRequestAndResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "https://abc123.xy.qweatherapi.com/weather/v1/current/39.920000/116.410000", request.URL.String())
		require.Equal(t, "secret-api-key", request.Header.Get("X-QW-Api-Key"))
		require.Empty(t, request.Header.Get("Authorization"))
		return reply(200, weatherJSON), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, "abc123.xy.qweatherapi.com", 0).GetCurrentWeather(), testConnection, beijing)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, GetCurrentWeatherBranchFound, result.Branch)
	require.Equal(t, "Few Clouds", result.Value.ConditionText)
	require.Equal(t, 31.71, result.Value.TemperatureValue)
	require.Equal(t, []string{"https://developer.qweather.com/attribution.html"}, result.Value.Attributions)
}
func TestGetCurrentWeatherClassifiesProviderOutcomes(t *testing.T) {
	scenarios := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"bad location", 400, `{"error":{"detail":"secret-api-key"}}`, GetCurrentWeatherBranchProviderRejected, false},
		{"unauthorized", 401, `{}`, GetCurrentWeatherBranchProviderRejected, false},
		{"invalid host", 403, `{}`, GetCurrentWeatherBranchProviderRejected, false},
		{"monthly limit", 429, `{"error":{"type":"https://dev.qweather.com/docs/resource/error-code/#over-monthly-limit"}}`, GetCurrentWeatherBranchProviderRejected, false},
		{"throttled", 429, `{"error":{"type":"https://dev.qweather.com/docs/resource/error-code/#too-many-requests"}}`, "", true},
		{"unavailable", 500, `{}`, "", true},
		{"malformed", 200, `{"condition":{}}`, GetCurrentWeatherBranchInvalidResponse, false},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(scenario.status, scenario.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, "abc123.xy.qweatherapi.com", 0).GetCurrentWeather(), testConnection, beijing)
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "secret-api-key")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "secret-api-key")
			}
		})
	}
}
func TestGetCurrentWeatherRejectsUnsafeCoordinatesAndHost(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, weatherJSON), nil })
	client := newTestClient(t, transport, "abc123.xy.qweatherapi.com", 0)
	for _, input := range []GetCurrentWeatherInput{{Latitude: 91}, {Latitude: -91}, {Latitude: math.NaN()}, {Longitude: 181}, {Longitude: -181}, {Longitude: math.Inf(1)}} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetCurrentWeather(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, GetCurrentWeatherBranchDefect, result.Branch)
	}
	for _, host := range []string{"api.qweather.com", "https://abc123.xy.qweatherapi.com", "abc123.xy.qweatherapi.com.evil", "abc123.xy.qweatherapi.com:443", "-abc.xy.qweatherapi.com"} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, host, 0).GetCurrentWeather(), testConnection, beijing)
		require.NoError(t, err)
		require.Equal(t, GetCurrentWeatherBranchDefect, result.Branch)
	}
	require.Zero(t, calls)
}
func TestGetCurrentWeatherBoundsResponseAndRedirects(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, "abc123.xy.qweatherapi.com", 16).GetCurrentWeather(), testConnection, beijing)
	require.NoError(t, err)
	require.Equal(t, GetCurrentWeatherBranchInvalidResponse, result.Branch)
	calls := 0
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		response := reply(302, "")
		response.Header.Set("Location", "https://other.example/steal")
		return response, nil
	})
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, "abc123.xy.qweatherapi.com", 0).GetCurrentWeather(), testConnection, beijing)
	require.NoError(t, err)
	require.Equal(t, GetCurrentWeatherBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
}
func TestGetCurrentWeatherTransportIsSecretSafe(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("secret-api-key abc123.xy.qweatherapi.com")
	})
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, "abc123.xy.qweatherapi.com", 0).GetCurrentWeather(), testConnection, beijing)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "secret-api-key")
	require.NotContains(t, retry.Error(), "abc123.xy.qweatherapi.com")
}
