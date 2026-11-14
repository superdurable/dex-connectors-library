// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package doc2x

import (
	"context"
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

var testConnection = sdkgo.ConnectionRef{Provider: "doc2x", Name: "doc2x"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "doc2x-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-parse-status" }
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
func TestGetParseStatusRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, parseStatusEndpoint, request.URL.Scheme+"://"+request.URL.Host+request.URL.Path)
		require.Equal(t, "task-123", request.URL.Query().Get("uid"))
		require.Equal(t, "Bearer test-secret", request.Header.Get("Authorization"))
		return reply(200, `{"code":"success","data":{"status":"success","progress":100,"result":{"pages":[{"md":"private document text"}]},"url":"https://secret.example"}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetParseStatus(), testConnection, GetParseStatusInput{UID: "task-123"})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, GetParseStatusBranchFound, result.Branch)
	require.Equal(t, GetParseStatusOutput{Status: "success", Progress: 100}, result.Value)
}
func TestGetParseStatusOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"processing", 200, `{"code":"success","data":{"status":"processing","progress":12}}`, GetParseStatusBranchFound, false},
		{"failed task", 200, `{"code":"success","data":{"status":"failed","detail":"private detail"}}`, GetParseStatusBranchFound, false},
		{"bad key", 401, `{"message":"test-secret"}`, GetParseStatusBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true}, {"server", 503, `{}`, "", true},
		{"business reject", 200, `{"code":"invalid_uid","data":{"status":"success"}}`, GetParseStatusBranchProviderRejected, false},
		{"unknown", 200, `{"code":"success","data":{"status":"queued"}}`, GetParseStatusBranchInvalidResponse, false},
		{"bad progress", 200, `{"code":"success","data":{"status":"success","progress":101}}`, GetParseStatusBranchInvalidResponse, false},
		{"redirect", 302, `{}`, GetParseStatusBranchProviderRejected, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				response := reply(scenario.status, scenario.body)
				if scenario.status == 302 {
					response.Header.Set("Location", "https://other.example/steal")
				}
				return response, nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetParseStatus(), testConnection, GetParseStatusInput{UID: "task-123"})
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
func TestGetParseStatusLocalAndTransport(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	client := newTestClient(t, transport, 16)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetParseStatus(), testConnection, GetParseStatusInput{UID: "task"})
	require.NoError(t, err)
	require.Equal(t, GetParseStatusBranchInvalidResponse, result.Branch)
	result, err = sdkgo.RunQuery(queryContext{}, client.GetParseStatus(), testConnection, GetParseStatusInput{UID: ""})
	require.NoError(t, err)
	require.Equal(t, GetParseStatusBranchDefect, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetParseStatus(), testConnection, GetParseStatusInput{UID: "task"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-secret")
}
