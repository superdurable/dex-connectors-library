// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package apifox

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

var testConnection = sdkgo.ConnectionRef{Provider: "apifox", Name: "apifox"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "apifox-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-project-api-summary" }
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
	credentials := Credentials{AccessToken: sdkgo.NewSecretString("test-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestGetProjectAPISummaryRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "https://api.apifox.com/v1/projects/123/export-openapi", request.URL.String())
		require.Equal(t, "Bearer test-secret", request.Header.Get("Authorization"))
		require.Equal(t, "2024-03-28", request.Header.Get("X-Apifox-Api-Version"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"scope":{"type":"ALL"},"oasVersion":"3.1","exportFormat":"JSON"}`, string(body))
		return reply(200, `{"openapi":"3.1.0","info":{"title":"Example API","version":"1.0.0","description":"private-description"},"paths":{"/one":{"get":{},"post":{}}},"message":"test-secret"}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetProjectAPISummary(), testConnection, GetProjectAPISummaryInput{ProjectID: "123"})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, GetProjectAPISummaryBranchFound, result.Branch)
	require.Equal(t, GetProjectAPISummaryOutput{Title: "Example API", Version: "1.0.0", EndpointCount: 2}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-")
	require.NotContains(t, string(encoded), "test-secret")
}
func TestGetProjectAPISummaryOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"openapi":"3.1.0","info":{"title":"Empty API","version":"1.0.0"},"paths":{}}`, GetProjectAPISummaryBranchFound, false},
		{"bad key", 401, `{"message":"test-secret"}`, GetProjectAPISummaryBranchProviderRejected, false},
		{"forbidden", 403, `{"message":"test-secret"}`, GetProjectAPISummaryBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true}, {"server", 503, `{}`, "", true},
		{"business reject", 200, `{"error":{"message":"test-secret"}}`, GetProjectAPISummaryBranchInvalidResponse, false},
		{"echoed token", 200, `{"openapi":"3.1.0","info":{"title":"test-secret","version":"1.0.0"},"paths":{}}`, GetProjectAPISummaryBranchInvalidResponse, false},
		{"missing list", 200, `{}`, GetProjectAPISummaryBranchInvalidResponse, false},
		{"null list", 200, `{"data":null}`, GetProjectAPISummaryBranchInvalidResponse, false},
		{"bad ID", 200, `{"data":[{"id":""}]}`, GetProjectAPISummaryBranchInvalidResponse, false},
		{"redirect", 302, `{}`, GetProjectAPISummaryBranchProviderRejected, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetProjectAPISummary(), testConnection, GetProjectAPISummaryInput{ProjectID: "123"})
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
func TestGetProjectAPISummaryRejectsInvalidProjectIDBeforeRequest(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid project ID reached provider")
		return nil, nil
	})
	for _, projectID := range []string{"", "0", "001", "../other", "123?x=1"} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetProjectAPISummary(), testConnection, GetProjectAPISummaryInput{ProjectID: projectID})
		require.NoError(t, err)
		require.Equal(t, GetProjectAPISummaryBranchDefect, result.Branch)
	}
}
func TestGetProjectAPISummaryOversizeAndTransport(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).GetProjectAPISummary(), testConnection, GetProjectAPISummaryInput{ProjectID: "123"})
	require.NoError(t, err)
	require.Equal(t, GetProjectAPISummaryBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetProjectAPISummary(), testConnection, GetProjectAPISummaryInput{ProjectID: "123"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-secret")
}
