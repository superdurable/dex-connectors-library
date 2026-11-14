// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package siliconflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAPIKey = "test-siliconflow-key-123"

var testConnection = sdkgo.ConnectionRef{Provider: "siliconflow", Name: "siliconflow"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "siliconflow-provider-test" }
func (queryContext) StepExecutionID() string     { return "listModels" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
	require.NoError(t, err)
	return client
}
func TestListModelsRequestAndResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v1/models", request.URL.Path)
		require.Equal(t, "Bearer "+testAPIKey, request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"object":"list","data":[{"id":"deepseek-ai/DeepSeek-V4-Flash","object":"model"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListModels(), testConnection, ListModelsInput{})
	require.NoError(t, err)
	require.Equal(t, ListModelsBranchListed, result.Branch)
	require.Equal(t, "deepseek-ai/DeepSeek-V4-Flash", result.Value.Models[0].ID)
}
func TestListModelsOutcomes(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"data":[]}`, ListModelsBranchListed, false},
		{"unauthorized", 401, `{"message":"private provider data"}`, ListModelsBranchProviderRejected, false},
		{"rate limit", 429, `private provider data`, "", true},
		{"server failure", 503, `private provider data`, "", true},
		{"malformed", 200, `{`, ListModelsBranchInvalidResponse, false},
		{"missing data", 200, `{"object":"list"}`, ListModelsBranchInvalidResponse, false},
		{"duplicate ID", 200, `{"data":[{"id":"x"},{"id":"x"}]}`, ListModelsBranchInvalidResponse, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(test.status)
				_, err := writer.Write([]byte(test.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListModels(), testConnection, ListModelsInput{})
			if test.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testAPIKey)
				require.NotContains(t, err.Error(), "private provider data")
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
		})
	}
}
func TestInvalidCredentialsSendNoRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		t.Error("unexpected request")
		writer.WriteHeader(200)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(strings.Repeat("x", 513))}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListModels(), testConnection, ListModelsInput{})
	require.NoError(t, err)
	require.Equal(t, ListModelsBranchDefect, result.Branch)
}

func TestResponseLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"data":[{"id":"` + strings.Repeat("x", 100) + `"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 30}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListModels(), testConnection, ListModelsInput{})
	require.NoError(t, err)
	require.Equal(t, ListModelsBranchInvalidResponse, result.Branch)
}

func TestTransportFailureRetriesWithoutLeakingKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := server.URL
	server.Close()
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, address).ListModels(), testConnection, ListModelsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testAPIKey)
}
