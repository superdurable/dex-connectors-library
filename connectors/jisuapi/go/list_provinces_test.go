// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jisuapi

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

var testConnection = sdkgo.ConnectionRef{Provider: "jisuapi", Name: "jisuapi"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "jisuapi-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-provinces" }
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
	credentials := Credentials{AppKey: sdkgo.NewSecretString("test-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestListProvincesRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "https://api.jisuapi.com/area/province?appkey=test-secret", request.URL.String())
		require.Equal(t, "test-secret", request.URL.Query().Get("appkey"))
		return reply(200, `{"status":0,"result":[{"id":"1","name":"Beijing","population":"private-cover"}],"msg":"private-trace"}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListProvinces(), testConnection, ListProvincesInput{})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, ListProvincesBranchListed, result.Branch)
	require.Equal(t, []Province{{ID: "1", Name: "Beijing"}}, result.Value.Provinces)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-cover")
	require.NotContains(t, string(encoded), "private-trace")
}
func TestListProvincesClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"business rejection", 200, `{"status":103,"msg":"test-secret"}`, ListProvincesBranchProviderRejected, false},
		{"unauthorized", 401, `{"message":"test-secret"}`, ListProvincesBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"provider quota", 200, `{"status":104,"msg":"test-secret"}`, "", true},
		{"IP rate limit", 200, `{"status":106,"msg":"test-secret"}`, "", true},
		{"maintenance", 200, `{"status":107,"msg":"test-secret"}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"missing status", 200, `{"result":[]}`, ListProvincesBranchInvalidResponse, false},
		{"missing result", 200, `{"status":0}`, ListProvincesBranchInvalidResponse, false},
		{"invalid province", 200, `{"status":0,"result":[{"id":"","name":"x"}]}`, ListProvincesBranchInvalidResponse, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(scenario.status, scenario.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListProvinces(), testConnection, ListProvincesInput{})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "test-secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "test-secret")
			}
		})
	}
}
func TestListProvincesBoundTransportAndRedirect(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).ListProvinces(), testConnection, ListProvincesInput{})
	require.NoError(t, err)
	require.Equal(t, ListProvincesBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListProvinces(), testConnection, ListProvincesInput{})
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
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListProvinces(), testConnection, ListProvincesInput{})
	require.NoError(t, err)
	require.Equal(t, ListProvincesBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
}
