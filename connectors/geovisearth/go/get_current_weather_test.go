// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package geovisearth

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

const testToken = "private-test-token"

var testConnection = sdkgo.ConnectionRef{Provider: "geovisearth", Name: "geovisearth"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "geovisearth-provider-test" }
func (queryContext) StepExecutionID() string     { return "current-weather" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type redirectTransport struct{ serverURL *url.URL }

func (transport redirectTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.URL.Scheme = transport.serverURL.Scheme
	cloned.URL.Host = transport.serverURL.Host
	return http.DefaultTransport.RoundTrip(cloned)
}
func newTestClient(t *testing.T, server *httptest.Server, maxResponseBytes int64) *Client {
	t.Helper()
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {Token: sdkgo.NewSecretString(testToken)}}
	client, err := New(Config{BaseURL: "https://api.open.geovisearth.com", MaxResponseBytes: maxResponseBytes}, credentials, WithHTTPClient(&http.Client{Transport: redirectTransport{serverURL}}))
	require.NoError(t, err)
	return client
}

const sampleWeather = `{"date":{"timeZone":"Asia/Shanghai","time":"20240911135031"},"result":{"wd_desc":"东北风","prs":986.7,"rh":95,"ws_desc":"3级","wp":"阴","tem":26.7,"wp_code":"02"},"location":{"path":"广西壮族自治区,崇左市,崇左","areaCode":"WTX_CH101300201"},"version":"v1","status":0}`

func TestCurrentWeatherRequestAndResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, weatherPath, request.URL.Path)
		require.Equal(t, testToken, request.URL.Query().Get("token"))
		require.Equal(t, "WTX_CH101300201", request.URL.Query().Get("location"))
		_, err := writer.Write([]byte(sampleWeather))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).GetCurrentWeather(), testConnection, GetCurrentWeatherInput{Location: "WTX_CH101300201"})
	require.NoError(t, err)
	require.Equal(t, GetCurrentWeatherBranchFetched, result.Branch)
	require.Equal(t, GetCurrentWeatherOutput{AreaCode: "WTX_CH101300201", Weather: "阴", WeatherCode: "02", TemperatureCelsius: 26.7, RelativeHumidityPercent: 95, WindDirection: "东北风", WindForce: "3级", SurfacePressureHpa: 986.7, ObservedAt: "20240911135031"}, result.Value)
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
		{"bad token", 401, "private", GetCurrentWeatherBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"forbidden", 403, "private", GetCurrentWeatherBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"provider code", 200, `{"status":3}`, GetCurrentWeatherBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"rate limit", 429, "private", "", sdkgo.FailureRateLimit, true},
		{"server", 503, "private", "", sdkgo.FailureAvailability, true},
		{"malformed", 200, "{", GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing result", 200, `{"status":0}`, GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"bad humidity", 200, `{"status":0,"date":{"time":"20240911135031"},"result":{"wp":"阴","wp_code":"02","tem":26.7,"rh":101,"wd_desc":"东北风","ws_desc":"3级","prs":986.7}}`, GetCurrentWeatherBranchInvalidResponse, sdkgo.FailureProtocol, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).GetCurrentWeather(), testConnection, GetCurrentWeatherInput{Location: "WTX_CH101300201"})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testToken)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, testToken)
			}
		})
	}
}
func TestCurrentWeatherLocationAndTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(sampleWeather))
		require.NoError(t, err)
	}))
	for _, location := range []string{"", "北京", "WTX_CHABC", "181,39", "116.4,91"} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).GetCurrentWeather(), testConnection, GetCurrentWeatherInput{Location: location})
		require.NoError(t, err)
		require.Equal(t, GetCurrentWeatherBranchDefect, result.Branch)
	}
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 8).GetCurrentWeather(), testConnection, GetCurrentWeatherInput{Location: "116.4,39.9"})
	require.NoError(t, err)
	require.Equal(t, GetCurrentWeatherBranchInvalidResponse, result.Branch)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).GetCurrentWeather(), testConnection, GetCurrentWeatherInput{Location: "WTX_CH101300201"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
func TestCurrentWeatherRejectsInsecureHost(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {Token: sdkgo.NewSecretString(testToken)}}
	_, err := New(Config{BaseURL: "http://api.open.geovisearth.com"}, credentials)
	require.Error(t, err)
	_, err = New(Config{BaseURL: "https://other.example"}, credentials)
	require.Error(t, err)
}
