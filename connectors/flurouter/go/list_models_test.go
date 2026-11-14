// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package flurouter

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

var testConnection = sdkgo.ConnectionRef{Provider: "flurouter", Name: "flurouter"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "flurouter-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-models" }
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
	credentials := Credentials{APIKey: sdkgo.NewSecretString("sk-il-test-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestListModelsRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, modelsEndpoint, request.URL.String())
		require.Empty(t, request.URL.RawQuery)
		require.Equal(t, "Bearer sk-il-test-secret", request.Header.Get("Authorization"))
		return reply(200, `{"object":"list","data":[{"id":"demo-chat","object":"model","private":"secret"},{"id":"demo-embedding","object":"model"}],"private":"secret"}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListModels(), testConnection, ListModelsInput{})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, ListModelsBranchFound, result.Branch)
	require.Equal(t, ListModelsOutput{ModelIDs: []string{"demo-chat", "demo-embedding"}}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret")
}
func TestListModelsClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty list", 200, `{"object":"list","data":[]}`, ListModelsBranchFound, false},
		{"unauthorized", 401, `{"error":{"message":"sk-il-test-secret"}}`, ListModelsBranchProviderRejected, false},
		{"forbidden", 403, `{"error":{"message":"sk-il-test-secret"}}`, ListModelsBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"missing list", 200, `{"object":"list"}`, ListModelsBranchInvalidResponse, false},
		{"wrong object", 200, `{"object":"other","data":[]}`, ListModelsBranchInvalidResponse, false},
		{"missing ID", 200, `{"object":"list","data":[{"object":"model"}]}`, ListModelsBranchInvalidResponse, false},
		{"wrong model object", 200, `{"object":"list","data":[{"id":"demo","object":"other"}]}`, ListModelsBranchInvalidResponse, false},
		{"duplicate ID", 200, `{"object":"list","data":[{"id":"demo","object":"model"},{"id":"demo","object":"model"}]}`, ListModelsBranchInvalidResponse, false},
		{"invalid ID", 200, `{"object":"list","data":[{"id":" bad","object":"model"}]}`, ListModelsBranchInvalidResponse, false},
		{"HTTP 302", 302, `{}`, ListModelsBranchProviderRejected, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListModels(), testConnection, ListModelsInput{})
			require.Equal(t, 1, calls)
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "sk-il-test-secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "sk-il-test-secret")
			}
		})
	}
}
func TestListModelsResponseBoundAndTransport(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).ListModels(), testConnection, ListModelsInput{})
	require.NoError(t, err)
	require.Equal(t, ListModelsBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("sk-il-test-secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListModels(), testConnection, ListModelsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "sk-il-test-secret")
}
func TestListModelsRejectsInvalidKeyWithoutRequest(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, `{}`), nil })
	credentials := Credentials{APIKey: sdkgo.NewSecretString("bad key")}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListModels(), testConnection, ListModelsInput{})
	require.NoError(t, err)
	require.Equal(t, ListModelsBranchDefect, result.Branch)
	require.Zero(t, calls)
}
