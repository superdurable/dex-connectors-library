// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hby365

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

var testConnection = sdkgo.ConnectionRef{Provider: "hby365", Name: "hby365"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "hby365-provider-test" }
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
	credentials := Credentials{APIKey: sdkgo.NewSecretString("test--secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestListModelsRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, modelListEndpoint, request.URL.String())
		require.Equal(t, "Bearer test--secret", request.Header.Get("Authorization"))
		return reply(200, `{"object":"list","data":[{"id":"example-model","object":"model","availability":{"is_available":true},"description":"private-description"}],"message":"test--secret"}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListModels(), testConnection, ListModelsInput{})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, ListModelsBranchFound, result.Branch)
	require.Equal(t, ListModelsOutput{Total: 1, ModelIDs: []string{"example-model"}}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-")
	require.NotContains(t, string(encoded), "test--secret")
}
func TestListModelsOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"object":"list","data":[]}`, ListModelsBranchFound, false},
		{"bad key", 401, `{"message":"test--secret"}`, ListModelsBranchProviderRejected, false},
		{"forbidden", 403, `{"message":"test--secret"}`, ListModelsBranchProviderRejected, false},
		{"rate limit", 429, `{"error":{"type":"rate_limit_reached_error"}}`, "", true}, {"server", 503, `{}`, "", true},
		{"business reject", 200, `{"error":{"message":"test--secret"}}`, ListModelsBranchInvalidResponse, false},
		{"missing list", 200, `{}`, ListModelsBranchInvalidResponse, false},
		{"null list", 200, `{"object":"list","data":null}`, ListModelsBranchInvalidResponse, false},
		{"bad ID", 200, `{"object":"list","data":[{"id":"","object":"model"}]}`, ListModelsBranchInvalidResponse, false},
		{"redirect", 302, `{}`, ListModelsBranchProviderRejected, false},
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
				require.NotContains(t, retry.Error(), "test--secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "test--secret")
			}
		})
	}
}
func TestListModelsOversizeAndTransport(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).ListModels(), testConnection, ListModelsInput{})
	require.NoError(t, err)
	require.Equal(t, ListModelsBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test--secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListModels(), testConnection, ListModelsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test--secret")
}
