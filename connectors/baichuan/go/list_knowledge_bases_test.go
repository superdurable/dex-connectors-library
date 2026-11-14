// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package baichuan

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

const testAPIKey = "test-baichuan-key-123"

var testConnection = sdkgo.ConnectionRef{Provider: "baichuan", Name: "baichuan"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "baichuan-provider-test" }
func (queryContext) StepExecutionID() string     { return "listKnowledgeBases" }
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

func TestListKnowledgeBasesRequestAndResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v1/kbs", request.URL.Path)
		require.Empty(t, request.URL.RawQuery)
		require.Equal(t, "Bearer "+testAPIKey, request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"object":"list","data":[{"id":"kb-abc123","object":"knowledge-base","name":"sample","description":"Example","file_ids":["file-123"],"created_at":1701828706,"updated_at":1701828706,"status":"online","split_type":"2"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListKnowledgeBases(), testConnection, ListKnowledgeBasesInput{})
	require.NoError(t, err)
	require.Equal(t, ListKnowledgeBasesBranchListed, result.Branch)
	require.Len(t, result.Value.KnowledgeBases, 1)
	require.Equal(t, "kb-abc123", result.Value.KnowledgeBases[0].ID)
	require.Equal(t, []string{"file-123"}, result.Value.KnowledgeBases[0].FileIDs)
	require.EqualValues(t, 1701828706, result.Value.KnowledgeBases[0].CreatedAt)
}
func TestListKnowledgeBasesOutcomes(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"object":"list","data":[]}`, ListKnowledgeBasesBranchListed, false},
		{"unauthorized", 401, `{"message":"private provider data"}`, ListKnowledgeBasesBranchProviderRejected, false},
		{"rate limit", 429, `private provider data`, "", true},
		{"server failure", 503, `private provider data`, "", true},
		{"malformed", 200, `{`, ListKnowledgeBasesBranchInvalidResponse, false},
		{"missing data", 200, `{"object":"list"}`, ListKnowledgeBasesBranchInvalidResponse, false},
		{"wrong envelope", 200, `{"object":"x","data":[]}`, ListKnowledgeBasesBranchInvalidResponse, false},
		{"wrong object", 200, `{"object":"list","data":[{"id":"kb-1","object":"file"}]}`, ListKnowledgeBasesBranchInvalidResponse, false},
		{"duplicate ID", 200, `{"object":"list","data":[{"id":"kb-1","object":"knowledge-base"},{"id":"kb-1","object":"knowledge-base"}]}`, ListKnowledgeBasesBranchInvalidResponse, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListKnowledgeBases(), testConnection, ListKnowledgeBasesInput{})
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
	result, err := sdkgo.RunQuery(queryContext{}, client.ListKnowledgeBases(), testConnection, ListKnowledgeBasesInput{})
	require.NoError(t, err)
	require.Equal(t, ListKnowledgeBasesBranchDefect, result.Branch)
}
func TestResponseLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"object":"list","data":[{"id":"` + strings.Repeat("x", 100) + `"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 30}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListKnowledgeBases(), testConnection, ListKnowledgeBasesInput{})
	require.NoError(t, err)
	require.Equal(t, ListKnowledgeBasesBranchInvalidResponse, result.Branch)
}
func TestTransportFailureRetriesWithoutLeakingKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := server.URL
	server.Close()
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, address).ListKnowledgeBases(), testConnection, ListKnowledgeBasesInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testAPIKey)
}
