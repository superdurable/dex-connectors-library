// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ppio

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

var testConnection = sdkgo.ConnectionRef{Provider: "ppio", Name: "ppio"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "ppio-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-models" }
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
func TestListModelsRequestAndResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "https://api.ppio.com/openai/v1/models", request.URL.String())
		require.Equal(t, "Bearer secret-api-key", request.Header.Get("Authorization"))
		return reply(200, `{"data":[{"id":"tencent/hy4","object":"model","created":1789747200,"owned_by":"ppio","display_name":"HY4","status":1,"context_size":131072,"endpoints":["chat/completions"]}]}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListModels(), testConnection, ListModelsInput{})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, ListModelsBranchFound, result.Branch)
	require.Equal(t, Model{ID: "tencent/hy4", CreatedAtUnix: 1789747200, OwnedBy: "ppio", DisplayName: "HY4", Status: 1, ContextSize: 131072, Endpoints: []string{"chat/completions"}}, result.Value.Models[0])
}
func TestListModelsClassifiesResponsesWithoutSecrets(t *testing.T) {
	scenarios := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"object":"list","data":[]}`, ListModelsBranchFound, false},
		{"unauthorized", 401, `{"error":"secret-api-key"}`, ListModelsBranchProviderRejected, false},
		{"forbidden", 403, `{}`, ListModelsBranchProviderRejected, false},
		{"malformed", 200, `{"object":"list","data":[{"id":"x"}]}`, ListModelsBranchInvalidResponse, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(scenario.status, scenario.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListModels(), testConnection, ListModelsInput{})
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
func TestListModelsBoundsBodyAndTransportIsSecretSafe(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).ListModels(), testConnection, ListModelsInput{})
	require.NoError(t, err)
	require.Equal(t, ListModelsBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("secret-api-key") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListModels(), testConnection, ListModelsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "secret-api-key")
}
func TestListModelsDoesNotFollowRedirect(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		response := reply(302, "")
		response.Header.Set("Location", "https://other.example/steal")
		return response, nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListModels(), testConnection, ListModelsInput{})
	require.NoError(t, err)
	require.Equal(t, ListModelsBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
}
