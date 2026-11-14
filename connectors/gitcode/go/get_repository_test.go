// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gitcode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAccessToken = "test-personal-access-token"

var testConnection = sdkgo.ConnectionRef{Provider: "gitcode", Name: "gitcode"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "gitcode-provider-test" }
func (queryContext) StepExecutionID() string     { return "repository" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessToken: sdkgo.NewSecretString(testAccessToken)}})
	require.NoError(t, err)
	return client
}
func TestBearerReadAndPrivateFieldsDropped(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/v5/repos/example/sample", request.URL.Path)
		require.Empty(t, request.URL.RawQuery)
		require.Equal(t, "Bearer "+testAccessToken, request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		_, err := writer.Write([]byte(`{"id":12345,"full_name":"example/sample","private":true,"default_branch":"main","creator":{"email":"private@example.com"},"members":["private-member"],"description":"private description"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetRepository(), testConnection, GetRepositoryInput{Owner: "example", Repo: "sample"})
	require.NoError(t, err)
	require.Equal(t, GetRepositoryBranchFetched, result.Branch)
	require.Equal(t, GetRepositoryOutput{ID: 12345, FullName: "example/sample", Private: true, DefaultBranch: "main"}, result.Value)
	require.EqualValues(t, 1, calls.Load())
}
func TestProviderOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"not found", 404, `{"message":"private"}`, GetRepositoryBranchNotFound, false},
		{"invalid request", 400, `{"message":"private"}`, GetRepositoryBranchProviderRejected, false},
		{"wrong token", 401, `{"message":"private"}`, GetRepositoryBranchProviderRejected, false},
		{"forbidden", 403, `{"message":"private"}`, GetRepositoryBranchProviderRejected, false},
		{"rate limit", 429, `{"message":"private"}`, "", true},
		{"server error", 503, `{"message":"private"}`, "", true},
		{"malformed", 200, `{`, GetRepositoryBranchInvalidResponse, false},
		{"missing privacy", 200, `{"id":12345,"full_name":"example/sample"}`, GetRepositoryBranchInvalidResponse, false},
		{"missing name", 200, `{"id":12345,"private":false}`, GetRepositoryBranchInvalidResponse, false},
		{"invalid id", 200, `{"id":0,"full_name":"example/sample","private":false}`, GetRepositoryBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(tc.status)
				_, err := writer.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetRepository(), testConnection, GetRepositoryInput{Owner: "example", Repo: "sample"})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testAccessToken)
				require.NotContains(t, err.Error(), "private")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
			require.NotContains(t, result.Failure.Message, "private")
		})
	}
}
func TestInvalidInputSendsNoRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { calls.Add(1); writer.WriteHeader(http.StatusOK) }))
	defer server.Close()
	for _, input := range []GetRepositoryInput{{}, {Owner: "example", Repo: "a/b"}, {Owner: "..", Repo: "sample"}, {Owner: "example", Repo: " bad"}, {Owner: strings.Repeat("x", 129), Repo: "sample"}} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetRepository(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, GetRepositoryBranchDefect, result.Branch)
	}
	require.Zero(t, calls.Load())
}
func TestOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"id":12345,"full_name":"example/sample","private":false}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 4}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessToken: sdkgo.NewSecretString(testAccessToken)}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetRepository(), testConnection, GetRepositoryInput{Owner: "example", Repo: "sample"})
	require.NoError(t, err)
	require.Equal(t, GetRepositoryBranchInvalidResponse, result.Branch)
}
