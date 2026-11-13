// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bocha

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

var testConnection = sdkgo.ConnectionRef{Provider: "bocha", Name: "bocha"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "bocha-provider-test" }
func (queryContext) StepExecutionID() string     { return "search-web" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(token any) any         { return context.Background().Value(token) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func newTestClient(t *testing.T, transport http.RoundTripper, limit int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("test-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestSearchWebRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, webSearchEndpoint, request.URL.String())
		require.Equal(t, "Bearer test-secret", request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		var payload map[string]any
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		require.Equal(t, map[string]any{"query": "test query", "count": float64(5), "summary": false}, payload)
		return reply(200, `{"code":200,"data":{"webPages":{"value":[{"name":"Example","url":"https://example.com/a","snippet":"A snippet","summary":"private-summary","siteIcon":"private-icon"}]}}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).SearchWeb(), testConnection, SearchWebInput{Query: "test query"})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, SearchWebBranchFound, result.Branch)
	require.Equal(t, SearchWebOutput{Results: []WebResult{{Title: "Example", URL: "https://example.com/a", Snippet: "A snippet"}}}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-")
}
func TestSearchWebOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"no results", 200, `{"code":200,"data":{"webPages":{"value":[]}}}`, SearchWebBranchFound, false},
		{"bad query", 400, `{"message":"test-secret"}`, SearchWebBranchProviderRejected, false},
		{"invalid key", 401, `{"message":"test-secret"}`, SearchWebBranchProviderRejected, false},
		{"no credit", 403, `{"message":"test-secret"}`, SearchWebBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"server", 500, `{}`, "", true},
		{"business rejection", 200, `{"code":403,"msg":"test-secret"}`, SearchWebBranchProviderRejected, false},
		{"missing code", 200, `{"data":{}}`, SearchWebBranchInvalidResponse, false},
		{"missing pages", 200, `{"code":200,"data":{}}`, SearchWebBranchInvalidResponse, false},
		{"invalid URL", 200, `{"code":200,"data":{"webPages":{"value":[{"name":"A","url":"javascript:bad","snippet":"B"}]}}}`, SearchWebBranchInvalidResponse, false},
		{"redirect", 302, `{}`, SearchWebBranchProviderRejected, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				response := reply(scenario.status, scenario.body)
				if scenario.status == 302 {
					response.Header.Set("Location", "https://other.example/steal")
				}
				return response, nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).SearchWeb(), testConnection, SearchWebInput{Query: "hello"})
			require.Equal(t, 1, calls)
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
func TestSearchWebLocalValidationAndTransport(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, `{}`), nil })
	client := newTestClient(t, transport, 0)
	for _, query := range []string{"", "  ", strings.Repeat("x", 257)} {
		result, err := sdkgo.RunQuery(queryContext{}, client.SearchWeb(), testConnection, SearchWebInput{Query: query})
		require.NoError(t, err)
		require.Equal(t, SearchWebBranchDefect, result.Branch)
	}
	require.Zero(t, calls)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).SearchWeb(), testConnection, SearchWebInput{Query: "hello"})
	require.NoError(t, err)
	require.Equal(t, SearchWebBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).SearchWeb(), testConnection, SearchWebInput{Query: "hello"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-secret")
}
