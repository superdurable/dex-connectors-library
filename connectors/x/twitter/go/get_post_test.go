// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package x

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

var testConnection = sdkgo.ConnectionRef{Provider: "x-twitter", Name: "x"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "x-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-post" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{AccessToken: sdkgo.NewSecretString("sample-x-access-token")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetPostReadsSelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/tweets/1845123456789012345", request.URL.Path)
		require.Equal(t, "Bearer sample-x-access-token", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"data":{"id":"1845123456789012345","text":"Example public Post"},"includes":{"users":[{"name":"not returned"}]}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetPost(), testConnection, GetPostInput{PostID: "1845123456789012345"})
	require.NoError(t, err)
	require.Equal(t, GetPostBranchFound, result.Branch)
	require.Equal(t, PostMetadata{ID: "1845123456789012345", Text: "Example public Post"}, result.Value)
}

func TestGetPostClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: GetPostBranchNotFound},
		{name: "forbidden", status: 403, branch: GetPostBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"data":{"id":"1845123456789012345","text":""}}`, branch: GetPostBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetPost(), testConnection, GetPostInput{PostID: "1845123456789012345"})
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

func TestGetPostRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://api.x.com/2", 0)
	for _, postID := range []string{"", "../other", "bad\npost", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetPost(), testConnection, GetPostInput{PostID: postID})
		require.NoError(t, err)
		require.Equal(t, GetPostBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"data":{"id":"1845123456789012345","text":"Example public Post"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 20).GetPost(), testConnection, GetPostInput{PostID: "1845123456789012345"})
	require.NoError(t, err)
	require.Equal(t, GetPostBranchInvalidResponse, result.Branch)
}
