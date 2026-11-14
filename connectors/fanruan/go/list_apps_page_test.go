// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fanruan

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

var testConnection = sdkgo.ConnectionRef{Provider: "fanruan", Name: "fanruan"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "fanruan-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-apps-page" }
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
	credentials := Credentials{APIKey: sdkgo.NewSecretString("secret-api-key")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestListAppsPageRequestAndSafeResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, appsEndpoint, request.URL.String())
		require.Equal(t, "Bearer secret-api-key", request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"limit":2,"skip":3}`, string(body))
		return reply(200, `{"apps":[{"app_id":"app-1","name":"采购系统","private":"secret-record"},{"app_id":"app-2","name":"库存系统"}]}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListAppsPage(), testConnection, ListAppsPageInput{Skip: 3, Limit: 2})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, ListAppsPageBranchListed, result.Branch)
	require.Equal(t, []AppMetadata{{ID: "app-1", Name: "采购系统"}, {ID: "app-2", Name: "库存系统"}}, result.Value.Apps)
	require.Equal(t, 5, result.Value.NextSkip)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret-record")
}
func TestListAppsPageClassifiesOutcomes(t *testing.T) {
	scenarios := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"apps":[]}`, ListAppsPageBranchListed, false},
		{"bad key", 400, `{"code":8101,"msg":"secret-api-key"}`, ListAppsPageBranchProviderRejected, false},
		{"rate code", 400, `{"code":8303,"msg":"secret-api-key"}`, "", true},
		{"concurrency", 429, `{}`, "", true},
		{"gateway", 502, `{}`, "", true},
		{"missing apps", 200, `{}`, ListAppsPageBranchInvalidResponse, false},
		{"missing id", 200, `{"apps":[{"name":"采购"}]}`, ListAppsPageBranchInvalidResponse, false},
		{"unauthorized", 401, `{}`, ListAppsPageBranchProviderRejected, false},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(scenario.status, scenario.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListAppsPage(), testConnection, ListAppsPageInput{})
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
func TestListAppsPageRejectsInvalidPaginationAndBoundsResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, strings.Repeat("x", 100)), nil })
	client := newTestClient(t, transport, 16)
	for _, input := range []ListAppsPageInput{{Skip: -1}, {Skip: int(^uint(0) >> 1)}, {Limit: -1}, {Limit: 101}} {
		result, err := sdkgo.RunQuery(queryContext{}, client.ListAppsPage(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, ListAppsPageBranchDefect, result.Branch)
	}
	require.Zero(t, calls)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListAppsPage(), testConnection, ListAppsPageInput{})
	require.NoError(t, err)
	require.Equal(t, ListAppsPageBranchInvalidResponse, result.Branch)
}
func TestListAppsPageTransportAndRedirectAreSecretSafe(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("secret-api-key") })
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListAppsPage(), testConnection, ListAppsPageInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "secret-api-key")
	calls := 0
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		response := reply(302, "")
		response.Header.Set("Location", "https://other.example/steal")
		return response, nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListAppsPage(), testConnection, ListAppsPageInput{})
	require.NoError(t, err)
	require.Equal(t, ListAppsPageBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
}
