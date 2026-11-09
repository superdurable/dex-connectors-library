// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reddit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAccessToken = "synthetic-reddit-token"

var testConnection = sdkgo.ConnectionRef{Provider: "reddit", Name: "reddit"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "reddit-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	credentials := Credentials{
		OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token"),
	}
	client, err := New(Config{BaseURL: baseURL, RedditUsername: "example"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetPostProjectsProviderPostAndCredentialsStaySecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/api/info", r.URL.Path)
		require.Equal(t, "t3_abc123", r.URL.Query().Get("id"))
		require.Equal(t, "1", r.URL.Query().Get("raw_json"))
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		require.Equal(t, "desktop:client-id:v0.1.0 (by /u/example)", r.Header.Get("User-Agent"))
		_, err := w.Write([]byte(`{"kind":"Listing","data":{"children":[{"kind":"t3","data":{"name":"t3_abc123","title":"Example post","subreddit":"example","permalink":"/r/example/comments/abc123/example_post/","score":42,"author":"not-persisted"}}]}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetPost(), testConnection, GetPostInput{PostID: "t3_abc123"})
	require.NoError(t, err)
	require.Equal(t, GetPostBranchFound, result.Branch)
	require.Equal(t, Post{ID: "t3_abc123", Title: "Example post", Subreddit: "example", Permalink: "/r/example/comments/abc123/example_post/", Score: 42}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "not-persisted")
	require.NotContains(t, string(encoded), testAccessToken)
}

func TestGetPostRejectsInvalidIDBeforeProviderCall(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetPost(), testConnection, GetPostInput{PostID: "invalid"})
	require.NoError(t, err)
	require.Equal(t, GetPostBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
