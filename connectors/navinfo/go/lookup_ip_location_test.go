// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package navinfo

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

var testConnection = sdkgo.ConnectionRef{Provider: "navinfo", Name: "navinfo"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "navinfo-provider-test" }
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
	credentials := Credentials{AK: sdkgo.NewSecretString("test-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestLookupIPLocationRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "https://lbs.navinfo.com/open-api/position/v1/ip?ak=test-secret&ip=118.28.8.8", request.URL.String())
		require.Equal(t, "test-secret", request.URL.Query().Get("ak"))
		return reply(200, `{"status":200,"data":{"prov":"北京市","city":"北京市","adcode":"110000","private":"private-cover"},"msg":"private-trace"}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).LookupIPLocation(), testConnection, LookupIPLocationInput{IPAddress: "118.28.8.8"})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, LookupIPLocationBranchFound, result.Branch)
	require.Equal(t, LookupIPLocationOutput{Province: "北京市", City: "北京市", Adcode: "110000"}, result.Value)
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
		{"business rejection", 200, `{"status":1001,"msg":"test-secret"}`, LookupIPLocationBranchProviderRejected, false},
		{"temporary quota", 200, `{"status":1008}`, "", true},
		{"unauthorized", 401, `{"msg":"test-secret"}`, LookupIPLocationBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"missing ret", 200, `{"data":{}}`, LookupIPLocationBranchInvalidResponse, false},
		{"missing info", 200, `{"status":200}`, LookupIPLocationBranchInvalidResponse, false},
		{"missing country", 200, `{"status":200,"data":"bad"}`, LookupIPLocationBranchInvalidResponse, false},
		{"daily quota", 200, `{"status":1007}`, LookupIPLocationBranchProviderRejected, false},
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
