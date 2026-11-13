// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package caiyun

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const testAppKey = "test-app-key"
const testAppSecret = "test-app-secret"

var testConnection = sdkgo.ConnectionRef{Provider: "caiyun", Name: "caiyun"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "caiyun-provider-test" }
func (queryContext) StepExecutionID() string     { return "current-weather" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppKey: sdkgo.NewSecretString(testAppKey), AppSecret: sdkgo.NewSecretString(testAppSecret)}})
	require.NoError(t, err)
	return client
}

var beijing = GetCurrentWeatherInput{Longitude: 116.3176, Latitude: 39.9760}

const validWeather = `{"status":"ok","result":{"realtime":{"status":"ok","temperature":20,"humidity":0.5,"skycon":"CLEAR_DAY","wind":{"speed":2}}}}`

func TestWeatherSignatureOfficialVector(t *testing.T) {
	require.Equal(t, "KfHsk3z2XfX6Yxox4Uf_VgyM0wHk6bWEyRqZ9QOJUYw=", signWeatherRequest("your_app_key", "your_app_secret", "/v2.6/your_app_key/116.3176,39.9760/weather", "alert=true&dailysteps=1&hourlysteps=24", "0195c68a-42e7-7243-bff2-ac97a78b837d", "1742791910"))
}

func TestCurrentWeatherSignedRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v2.6/test-app-key/116.3176,39.976/realtime", request.URL.Path)
		require.Empty(t, request.URL.RawQuery)
		nonce := request.Header.Get("x-cy-nonce")
		require.Len(t, nonce, 32)
		timestamp := request.Header.Get("x-cy-timestamp")
		require.NotEmpty(t, timestamp)
		require.Equal(t, signWeatherRequest(testAppKey, testAppSecret, request.URL.Path, "", nonce, timestamp), request.Header.Get("x-cy-signature"))
		_, err := writer.Write([]byte(validWeather))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetCurrentWeather(), testConnection, beijing)
	require.NoError(t, err)
	require.Equal(t, GetCurrentWeatherBranchFetched, result.Branch)
	require.Equal(t, GetCurrentWeatherOutput{20, 0.5, "CLEAR_DAY", 2}, result.Value)
	require.EqualValues(t, 1, calls.Load())
}
func TestCurrentWeatherProviderOutcomes(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		retryAfter string
		branch     sdkgo.BranchID
		retry      bool
	}{
		{"bad key", 400, `{"status":"failed","error":"token is invalid"}`, "", GetCurrentWeatherBranchProviderRejected, false},
		{"no permission", 401, `{"status":"failed"}`, "", GetCurrentWeatherBranchProviderRejected, false},
		{"forbidden", 403, `{"status":"failed"}`, "", GetCurrentWeatherBranchProviderRejected, false},
		{"bad coordinate", 422, `{"status":"failed"}`, "", GetCurrentWeatherBranchProviderRejected, false},
		{"quota", 429, `{"status":"failed","error":"quota exhausted"}`, "", GetCurrentWeatherBranchProviderRejected, false},
		{"qps", 429, "Rate limit exceeded", "2", "", true},
		{"server", 503, "private provider data", "2", "", true},
		{"malformed", 200, "{", "", GetCurrentWeatherBranchInvalidResponse, false},
		{"missing", 200, `{"status":"ok","result":{}}`, "", GetCurrentWeatherBranchInvalidResponse, false},
		{"failed 200", 200, `{"status":"failed","error":"private provider data"}`, "", GetCurrentWeatherBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				if tc.retryAfter != "" {
					writer.Header().Set("Retry-After", tc.retryAfter)
				}
				writer.WriteHeader(tc.status)
				_, err := writer.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetCurrentWeather(), testConnection, beijing)
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testAppSecret)
				require.NotContains(t, err.Error(), "private provider data")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestInvalidCoordinateSendsNoRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { calls.Add(1); writer.WriteHeader(200) }))
	defer server.Close()
	for _, input := range []GetCurrentWeatherInput{{Longitude: 181}, {Latitude: -91}, {Longitude: math.NaN()}, {Latitude: math.Inf(1)}} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetCurrentWeather(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, GetCurrentWeatherBranchDefect, result.Branch)
	}
	require.Zero(t, calls.Load())
}
func TestOversizedResponseAndTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(validWeather))
		require.NoError(t, err)
	}))
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppKey: sdkgo.NewSecretString(testAppKey), AppSecret: sdkgo.NewSecretString(testAppSecret)}}
	client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 8}, credentials)
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetCurrentWeather(), testConnection, beijing)
	require.NoError(t, err)
	require.Equal(t, GetCurrentWeatherBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetCurrentWeather(), testConnection, beijing)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testAppSecret)
}
