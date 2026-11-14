// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package infiniai

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

var testConnection = sdkgo.ConnectionRef{Provider: "infiniai", Name: "infiniai"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "infiniai-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-models" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func newTestClient(t *testing.T, transport http.RoundTripper, apiKey string, limit int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString(apiKey)}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}

const modelsJSON = `{"object":"list","data":[{"id":"deepseek-v4-pro","object":"model","created":0,"owned_by":"","max_output_length":393216,"context_length":1024000,"model_type":"大语言模型"},{"id":"bge-m3","object":"model","created":0,"owned_by":"","max_output_length":0,"context_length":8192,"model_type":"向量模型"}]}`

func TestListModelsRequestAndResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, modelListEndpoint, request.URL.String())
		require.Equal(t, "Bearer sk-secret-api-key", request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		return reply(200, modelsJSON), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, "sk-secret-api-key", 0).ListModels(), testConnection, ListModelsInput{})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, ListModelsBranchListed, result.Branch)
	require.Equal(t, []ModelInfo{{ID: "deepseek-v4-pro", ModelType: "大语言模型", ContextLength: 1024000, MaxOutputLength: 393216}, {ID: "bge-m3", ModelType: "向量模型", ContextLength: 8192}}, result.Value.Models)
}
func TestListModelsClassifiesProviderOutcomes(t *testing.T) {
	scenarios := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{{"bad request", 400, `{"error":{"message":"sk-secret-api-key"}}`, ListModelsBranchProviderRejected, false}, {"unauthorized", 401, `{}`, ListModelsBranchProviderRejected, false}, {"forbidden", 403, `{}`, ListModelsBranchProviderRejected, false}, {"rate limited", 429, `{}`, "", true}, {"unavailable", 503, `{}`, "", true}, {"malformed", 200, `{}`, ListModelsBranchInvalidResponse, false}, {"missing limit", 200, `{"object":"list","data":[{"id":"x","object":"model","model_type":"大语言模型"}]}`, ListModelsBranchInvalidResponse, false}, {"wrong object", 200, `{"object":"data","data":[]}`, ListModelsBranchInvalidResponse, false}}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(scenario.status, scenario.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, "sk-secret-api-key", 0).ListModels(), testConnection, ListModelsInput{})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "sk-secret-api-key")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "sk-secret-api-key")
			}
		})
	}
}
func TestListModelsRejectsUnsafeKeyWithoutRequest(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, modelsJSON), nil })
	for _, key := range []string{"", "secret", "sk-bad key", "sk-key\nInjected: yes"} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, key, 0).ListModels(), testConnection, ListModelsInput{})
		require.NoError(t, err)
		require.Equal(t, ListModelsBranchDefect, result.Branch)
	}
	require.Zero(t, calls)
}
func TestListModelsBoundsRedirectAndTransport(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, "sk-secret-api-key", 16).ListModels(), testConnection, ListModelsInput{})
	require.NoError(t, err)
	require.Equal(t, ListModelsBranchInvalidResponse, result.Branch)
	calls := 0
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		response := reply(302, "")
		response.Header.Set("Location", "https://evil.example/collect")
		return response, nil
	})
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, "sk-secret-api-key", 0).ListModels(), testConnection, ListModelsInput{})
	require.NoError(t, err)
	require.Equal(t, ListModelsBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("sk-secret-api-key in transport") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, "sk-secret-api-key", 0).ListModels(), testConnection, ListModelsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "sk-secret-api-key")
}
