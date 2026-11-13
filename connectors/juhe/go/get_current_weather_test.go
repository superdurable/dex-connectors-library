// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package juhe

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

const testAPIKey = "test-juhe-api-key"

var testConnection = sdkgo.ConnectionRef{Provider: "juhe", Name: "juhe"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "juhe-provider-test" }
func (queryContext) StepExecutionID() string     { return "current-weather" }
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
func TestCurrentWeatherRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/simpleWeather/query", request.URL.Path)
		require.Equal(t, testAPIKey, request.URL.Query().Get("key"))
		require.Equal(t, "北京", request.URL.Query().Get("city"))
		_, err := writer.Write([]byte(`{"reason":"查询成功","error_code":0,"result":{"city":"北京","realtime":{"info":"晴","wid":"00","temperature":"20","humidity":"45","direct":"北风","power":"2级","aqi":"50"},"future":[{"private":"omitted"}]}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetCurrentWeather(), testConnection, GetCurrentWeatherInput{City: "北京"})
	require.NoError(t, err)
	require.Equal(t, GetCurrentWeatherBranchFetched, result.Branch)
	require.Equal(t, GetCurrentWeatherOutput{City: "北京", WeatherText: "晴", WeatherCode: "00", TemperatureCelsius: "20", HumidityPercent: "45", WindDirection: "北风", WindPower: "2级", AirQualityIndex: "50"}, result.Value)
	require.EqualValues(t, 1, calls.Load())
}
func TestCurrentWeatherOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"bad key", 200, `{"error_code":10001}`, GetCurrentWeatherBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"expired key", 200, `{"error_code":10003}`, GetCurrentWeatherBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"unauthorized", 200, `{"error_code":10002}`, GetCurrentWeatherBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"daily quota", 200, `{"error_code":10012}`, GetCurrentWeatherBranchProviderRejected, sdkgo.FailureQuotaExhausted, false},
		{"unknown city", 200, `{"error_code":207302}`, GetCurrentWeatherBranchProviderRejected, sdkgo.FailureValidation, false},
		{"service network", 200, `{"error_code":207303}`, "", sdkgo.FailureAvailability, true},
		{"rate limit", 429, `provider private text`, "", sdkgo.FailureRateLimit, true},
		{"server unavailable", 503, `provider private text`, "", sdkgo.FailureAvailability, true},
		{"bad HTTP", 403, `provider private text`, GetCurrentWeatherBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"malformed JSON", 200, `{`, GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing error code", 200, `{"result":{}}`, GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing city", 200, `{"error_code":0,"result":{"realtime":{"info":"晴","wid":"00"}}}`, GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureProtocol, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetCurrentWeather(), testConnection, GetCurrentWeatherInput{City: "北京"})
			if tc.retry {
				var retryAfter *dex.RetryAfterError
				if tc.status == 429 || tc.status == 503 {
					require.ErrorAs(t, err, &retryAfter)
					require.Equal(t, 2*time.Second, retryAfter.After)
				} else {
					var retry *sdkgo.RetryError
					require.ErrorAs(t, err, &retry)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
			}
			if err != nil {
				require.NotContains(t, err.Error(), testAPIKey)
				require.NotContains(t, err.Error(), "provider private text")
			}
		})
	}
}
func TestInvalidCitySendsNoRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { calls.Add(1) }))
	defer server.Close()
	for _, city := range []string{"", " 北京", strings.Repeat("x", 129)} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetCurrentWeather(), testConnection, GetCurrentWeatherInput{City: city})
		require.NoError(t, err)
		require.Equal(t, GetCurrentWeatherBranchDefect, result.Branch)
	}
	require.Zero(t, calls.Load())
}

func TestOversizedResponseAndTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"error_code":0,"result":{"city":"北京","realtime":{"info":"晴","wid":"00"}}}`))
		require.NoError(t, err)
	}))
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
	client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 8}, credentials)
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetCurrentWeather(), testConnection, GetCurrentWeatherInput{City: "北京"})
	require.NoError(t, err)
	require.Equal(t, GetCurrentWeatherBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetCurrentWeather(), testConnection, GetCurrentWeatherInput{City: "北京"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.NotContains(t, err.Error(), testAPIKey)
}
