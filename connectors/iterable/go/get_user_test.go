// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package iterable

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "iterable", Name: "iterable"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "iterable-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-user" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("sample-iterable-api-key")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetUserReadsSelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/users/user@example.com", request.URL.Path)
		require.Equal(t, "sample-iterable-api-key", request.Header.Get("Api-Key"))
		_, err := writer.Write([]byte(`{"user":{"email":"user@example.com","userId":"example-user-id","dataFields":{"private":"secret"}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetUser(), testConnection, GetUserInput{Email: "user@example.com"})
	require.NoError(t, err)
	require.Equal(t, GetUserBranchFound, result.Branch)
	require.Equal(t, UserIdentity{Email: "user@example.com", UserID: "example-user-id"}, result.Value)
}

func TestGetUserClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: GetUserBranchNotFound},
		{name: "forbidden", status: 403, branch: GetUserBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"user":{"email":"different@example.com"}}`, branch: GetUserBranchInvalidResponse},
		{name: "unknown user empty object", status: 200, body: `{}`, branch: GetUserBranchNotFound},
		{name: "unknown user null", status: 200, body: `{"user":null}`, branch: GetUserBranchNotFound},
		{name: "unknown user empty user", status: 200, body: `{"user":{}}`, branch: GetUserBranchNotFound},
		{name: "user is not an object", status: 200, body: `{"user":"user@example.com"}`, branch: GetUserBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetUser(), testConnection, GetUserInput{Email: "user@example.com"})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
}

func TestGetUserRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://api.iterable.com/api", 0)
	for _, email := range []string{"", "../other", "bad\nuser", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetUser(), testConnection, GetUserInput{Email: email})
		require.NoError(t, err)
		require.Equal(t, GetUserBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"user":{"email":"user@example.com","userId":"example-user-id"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 20).GetUser(), testConnection, GetUserInput{Email: "user@example.com"})
	require.NoError(t, err)
	require.Equal(t, GetUserBranchInvalidResponse, result.Branch)
}
