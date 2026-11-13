// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dataeyes

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

var testConnection = sdkgo.ConnectionRef{Provider: "dataeyes", Name: "dataeyes"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "dataeyes-provider-test" }
func (queryContext) StepExecutionID() string     { return "read-web-page" }
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
func TestReadWebPageRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, readerEndpoint, request.URL.String())
		require.Equal(t, "Bearer test-secret", request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		var payload map[string]any
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		require.Equal(t, map[string]any{"url": "https://example.com", "format": "markdown"}, payload)
		return reply(200, `{"code":0,"data":{"metadata":{"title":"Example Domain","url":"private-url","favicon":"private-icon"},"html":"private-html","markdown":"# Example"},"message":"success"}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ReadWebPage(), testConnection, ReadWebPageInput{URL: "https://example.com"})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, ReadWebPageBranchFound, result.Branch)
	require.Equal(t, ReadWebPageOutput{Title: "Example Domain", Markdown: "# Example"}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-")
}
func TestReadWebPageClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty content", 200, `{"code":0,"data":{"metadata":{"title":"Empty"},"markdown":""}}`, ReadWebPageBranchFound, false},
		{"bad URL", 500, `{"code":32000,"message":"test-secret"}`, ReadWebPageBranchProviderRejected, false},
		{"unreachable", 500, `{"code":32001,"message":"test-secret"}`, ReadWebPageBranchProviderRejected, false},
		{"timeout", 500, `{"code":32002,"message":"test-secret"}`, "", true},
		{"unsupported", 500, `{"code":32003,"message":"test-secret"}`, ReadWebPageBranchProviderRejected, false},
		{"invalid key", 401, `{"error":"test-secret"}`, ReadWebPageBranchProviderRejected, false},
		{"quota", 403, `{"error":"test-secret"}`, ReadWebPageBranchProviderRejected, false},
		{"rate limit", 429, `{"error":"test-secret"}`, "", true},
		{"server", 500, `{}`, "", true},
		{"missing code", 200, `{"data":{}}`, ReadWebPageBranchInvalidResponse, false},
		{"missing markdown", 200, `{"code":0,"data":{"metadata":{"title":"A"}}}`, ReadWebPageBranchInvalidResponse, false},
		{"redirect", 302, `{}`, ReadWebPageBranchProviderRejected, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ReadWebPage(), testConnection, ReadWebPageInput{URL: "https://example.com"})
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
func TestReadWebPageLocalValidationAndTransport(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, `{}`), nil })
	client := newTestClient(t, transport, 0)
	for _, target := range []string{"", "http://example.com", "https://localhost/a", "https://127.0.0.1/a", "https://example.com/?token=secret", "https://user:pass@example.com", "https://example.com/#secret"} {
		result, err := sdkgo.RunQuery(queryContext{}, client.ReadWebPage(), testConnection, ReadWebPageInput{URL: target})
		require.NoError(t, err)
		require.Equal(t, ReadWebPageBranchDefect, result.Branch)
	}
	require.Zero(t, calls)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).ReadWebPage(), testConnection, ReadWebPageInput{URL: "https://example.com"})
	require.NoError(t, err)
	require.Equal(t, ReadWebPageBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ReadWebPage(), testConnection, ReadWebPageInput{URL: "https://example.com"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-secret")
}
