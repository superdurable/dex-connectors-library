// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package alapi

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

var testConnection = sdkgo.ConnectionRef{Provider: "alapi", Name: "alapi"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "alapi-provider-test" }
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
	credentials := Credentials{Token: sdkgo.NewSecretString("private-test-key")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestLookupIPLocationQueriesProviderAndFiltersResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/api/ip", request.URL.Path)
		require.Equal(t, "private-test-key", request.Header.Get("token"))
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"ip":"8.8.8.8"}`, string(body))
		require.NotContains(t, string(body), "private-test-key")
		return reply(200, `{"success":true,"code":200,"message":"private-test-key","data":{"country":"中国","province":"广东","city":"深圳","district":"龙岗区","isp":"移动","private":"omit"}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).LookupIPLocation(), testConnection, LookupIPLocationInput{IPAddress: "8.8.8.8"})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, LookupIPLocationBranchFound, result.Branch)
	require.Equal(t, LookupIPLocationOutput{Country: "中国", Province: "广东", City: "深圳", District: "龙岗区", Operator: "移动"}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-test-key")
	require.NotContains(t, string(encoded), "private-id")
}
func TestLookupIPLocationClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"business rejection", 200, `{"success":false,"code":10002,"message":"private-test-key"}`, LookupIPLocationBranchProviderRejected, false},
		{"quota exhausted", 200, `{"success":false,"code":10005}`, LookupIPLocationBranchProviderRejected, false},
		{"unauthorized", 401, `{"msg":"private-test-key"}`, LookupIPLocationBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"malformed", 200, `{`, LookupIPLocationBranchInvalidResponse, false},
		{"missing code", 200, `{"data":{}}`, LookupIPLocationBranchInvalidResponse, false},
		{"missing result", 200, `{"success":true,"code":200}`, LookupIPLocationBranchInvalidResponse, false},
		{"missing city", 200, `{"success":true,"code":200,"data":{"country":"中国","province":"广东"}}`, LookupIPLocationBranchInvalidResponse, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(scenario.status, scenario.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).LookupIPLocation(), testConnection, LookupIPLocationInput{IPAddress: "8.8.8.8"})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "private-test-key")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			require.NotContains(t, result.Failure.Message, "private-test-key")
		})
	}
}
func TestLookupIPLocationBoundTransportAndInvalidInput(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).LookupIPLocation(), testConnection, LookupIPLocationInput{IPAddress: "8.8.8.8"})
	require.NoError(t, err)
	require.Equal(t, LookupIPLocationBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("private-test-key") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).LookupIPLocation(), testConnection, LookupIPLocationInput{IPAddress: "8.8.8.8"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "private-test-key")
	calls := 0
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, `{}`), nil })
	for _, address := range []string{"", "8.8.8.8/32", "192.168.1.1", "::1"} {
		result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).LookupIPLocation(), testConnection, LookupIPLocationInput{IPAddress: address})
		require.NoError(t, err)
		require.Equal(t, LookupIPLocationBranchDefect, result.Branch)
	}
	require.Zero(t, calls)
}
