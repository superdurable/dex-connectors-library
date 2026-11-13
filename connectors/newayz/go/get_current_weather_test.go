// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package newayz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAccessKey = "test-newayz-access-key"

var testConnection = sdkgo.ConnectionRef{Provider: "newayz", Name: "newayz"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "newayz-provider-test" }
func (queryContext) StepExecutionID() string     { return "current-weather" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type redirectTransport struct{ serverURL *url.URL }

func (transport redirectTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copied := request.Clone(request.Context())
	copied.URL.Scheme = transport.serverURL.Scheme
	copied.URL.Host = transport.serverURL.Host
	copied.Host = transport.serverURL.Host
	return http.DefaultTransport.RoundTrip(copied)
}
func newTestClient(t *testing.T, serverURL string, maxResponseBytes int64) *Client {
	t.Helper()
	parsed, err := url.Parse(serverURL)
	require.NoError(t, err)
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessKey: sdkgo.NewSecretString(testAccessKey)}}
	client, err := New(Config{MaxResponseBytes: maxResponseBytes}, credentials, WithHTTPClient(&http.Client{Transport: redirectTransport{parsed}}))
	require.NoError(t, err)
	return client
}

func TestCurrentWeatherRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/location/scenario/v1/weather", request.URL.Path)
		for key, want := range map[string]string{"access_key": testAccessKey, "city": "420100", "extensions": "base"} {
			require.Equal(t, want, request.URL.Query().Get(key), key)
		}
		_, err := writer.Write([]byte(`{"status":"1","infocode":"10000","lives":[{"province":"湖北省","city":"武汉市","adcode":"420100","weather":"晴","temperature":"20","winddirection":"东风","windpower":"2","humidity":"50","reporttime":"2026-01-01 12:00:00"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetCurrentWeather(), testConnection, GetCurrentWeatherInput{CityCode: "420100"})
	require.NoError(t, err)
	require.Equal(t, GetCurrentWeatherBranchFetched, result.Branch)
	require.Equal(t, "武汉市", result.Value.City)
	require.Equal(t, "20", result.Value.TemperatureCelsius)
	require.Equal(t, "50", result.Value.HumidityPercent)
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
		{"invalid key", 403, `private provider data`, GetCurrentWeatherBranchProviderRejected, false},
		{"provider rejection", 200, `{"status":"0","infocode":"10001"}`, GetCurrentWeatherBranchProviderRejected, false},
		{"http rate limit", 429, `private provider data`, "", true},
		{"server failure", 503, `private provider data`, "", true},
		{"malformed", 200, `{`, GetCurrentWeatherBranchInvalidResponse, false},
		{"missing lives", 200, `{"status":"1","infocode":"10000","lives":[]}`, GetCurrentWeatherBranchInvalidResponse, false},
		{"wrong city", 200, `{"status":"1","infocode":"10000","lives":[{"city":"武汉市","adcode":"110000","weather":"晴","temperature":"20"}]}`, GetCurrentWeatherBranchInvalidResponse, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetCurrentWeather(), testConnection, GetCurrentWeatherInput{CityCode: "420100"})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testAccessKey)
				require.NotContains(t, err.Error(), "private provider data")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}

func TestInvalidCityCodeSendsNoRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { calls.Add(1); writer.WriteHeader(http.StatusOK) }))
	defer server.Close()
	for _, cityCode := range []string{"", "42010", "420100 ", "abcdef"} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetCurrentWeather(), testConnection, GetCurrentWeatherInput{CityCode: cityCode})
		require.NoError(t, err)
		require.Equal(t, GetCurrentWeatherBranchDefect, result.Branch)
	}
	require.Zero(t, calls.Load())
}

func TestOversizedResponseAndTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(strings.Repeat("x", 100)))
		require.NoError(t, err)
	}))
	client := newTestClient(t, server.URL, 8)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetCurrentWeather(), testConnection, GetCurrentWeatherInput{CityCode: "420100"})
	require.NoError(t, err)
	require.Equal(t, GetCurrentWeatherBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetCurrentWeather(), testConnection, GetCurrentWeatherInput{CityCode: "420100"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.NotContains(t, err.Error(), testAccessKey)
}
