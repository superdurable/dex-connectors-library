// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ipip

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "ipip", Name: "ipip"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "ipip-provider-test" }
func (queryContext) StepExecutionID() string     { return "lookup-ip-location" }
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
func newTestClient(t *testing.T, transport http.RoundTripper, limit int64) *Client {
	t.Helper()
	credentials := Credentials{Token: sdkgo.NewSecretString("test-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestLookupIPLocationRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "https://ipapi.ipip.net/v2/query/118.28.8.8?token=test-secret", request.URL.String())
		require.Equal(t, "test-secret", request.URL.Query().Get("token"))
		return reply(200, `{"ret":"ok","data":{"ip":"118.28.8.8","info":{"country_name":"中国","region_name":"北京","city_name":"北京","country_code":"CN","district_info":{"private":"private-cover"}}},"msg":"private-trace"}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).LookupIPLocation(), testConnection, LookupIPLocationInput{IPAddress: "118.28.8.8"})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, LookupIPLocationBranchFound, result.Branch)
	require.Equal(t, LookupIPLocationOutput{CountryName: "中国", RegionName: "北京", CityName: "北京", CountryCode: "CN"}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-cover")
	require.NotContains(t, string(encoded), "private-trace")
}
func TestLookupIPLocationClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"business rejection", 200, `{"ret":"err","msg":"test-secret"}`, LookupIPLocationBranchProviderRejected, false},
		{"unauthorized", 401, `{"msg":"test-secret"}`, LookupIPLocationBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"missing ret", 200, `{"data":{}}`, LookupIPLocationBranchInvalidResponse, false},
		{"missing info", 200, `{"ret":"ok","data":{}}`, LookupIPLocationBranchInvalidResponse, false},
		{"missing country", 200, `{"ret":"ok","data":{"info":{"region_name":"x","city_name":"y","country_code":"CN"}}}`, LookupIPLocationBranchInvalidResponse, false},
		{"unknown ret", 200, `{"ret":"unknown"}`, LookupIPLocationBranchInvalidResponse, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(scenario.status, scenario.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).LookupIPLocation(), testConnection, LookupIPLocationInput{IPAddress: "118.28.8.8"})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "test-secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			require.NotContains(t, result.Failure.Message, "test-secret")
		})
	}
}
func TestLookupIPLocationBoundTransportAndRedirect(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).LookupIPLocation(), testConnection, LookupIPLocationInput{IPAddress: "118.28.8.8"})
	require.NoError(t, err)
	require.Equal(t, LookupIPLocationBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).LookupIPLocation(), testConnection, LookupIPLocationInput{IPAddress: "118.28.8.8"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-secret")
	calls := 0
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		response := reply(302, "")
		response.Header.Set("Location", "https://other.example/steal")
		return response, nil
	})
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).LookupIPLocation(), testConnection, LookupIPLocationInput{IPAddress: "118.28.8.8"})
	require.NoError(t, err)
	require.Equal(t, LookupIPLocationBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
}

func TestLookupIPLocationRejectsInvalidIPWithoutRequest(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, `{}`), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).LookupIPLocation(), testConnection, LookupIPLocationInput{IPAddress: "118.28.8.8/24"})
	require.NoError(t, err)
	require.Equal(t, LookupIPLocationBranchDefect, result.Branch)
	require.Zero(t, calls)
}
