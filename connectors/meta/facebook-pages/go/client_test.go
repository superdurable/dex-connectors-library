// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package facebookpages

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

const testPageToken = "synthetic-page-token"

var testConnection = sdkgo.ConnectionRef{Provider: "facebook-pages", Name: "facebook-pages"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "facebook-pages-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {PageAccessToken: sdkgo.NewSecretString(testPageToken)}})
	require.NoError(t, err)
	return client
}

func TestListPostsProjectsFeedAndCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/123456789/feed", r.URL.Path)
		require.Equal(t, "id,message,created_time,permalink_url", r.URL.Query().Get("fields"))
		require.Equal(t, "100", r.URL.Query().Get("limit"))
		require.Equal(t, "cursor/+value", r.URL.Query().Get("after"))
		require.Equal(t, "Bearer "+testPageToken, r.Header.Get("Authorization"))
		require.Empty(t, r.URL.Query().Get("access_token"))
		_, err := w.Write([]byte(`{"data":[{"id":"123456789_987654321","message":"Welcome","created_time":"2026-11-08T10:00:00+0000","permalink_url":"https://www.facebook.com/123456789/posts/987654321","comments":{"data":[{"message":"private"}]}}],"paging":{"cursors":{"after":"next-cursor"}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListPosts(), testConnection, ListPostsInput{PageID: "123456789", After: "cursor/+value"})
	require.NoError(t, err)
	require.Equal(t, ListPostsBranchListed, result.Branch)
	require.Equal(t, "next-cursor", result.Value.NextCursor)
	require.Equal(t, "Welcome", result.Value.Posts[0].Message)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
	require.NotContains(t, string(encoded), testPageToken)
}

func TestGetPostProjectsOnlyMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/123456789_987654321", r.URL.Path)
		require.Equal(t, "id,message,created_time,permalink_url", r.URL.Query().Get("fields"))
		_, err := w.Write([]byte(`{"id":"123456789_987654321","message":"Welcome","created_time":"2026-11-08T10:00:00+0000","permalink_url":"https://www.facebook.com/123456789/posts/987654321","comments":{"data":[{"message":"private"}]}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetPost(), testConnection, GetPostInput{PostID: "123456789_987654321"})
	require.NoError(t, err)
	require.Equal(t, GetPostBranchFound, result.Branch)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
}

func TestGraphFailuresAndValidation(t *testing.T) {
	var calls atomic.Int32
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(int(status.Load()))
		_, err := w.Write([]byte(`{"error":{"message":"unavailable"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	invalid, err := sdkgo.RunQuery(queryContext{}, client.ListPosts(), testConnection, ListPostsInput{PageID: "bad/id"})
	require.NoError(t, err)
	require.Equal(t, ListPostsBranchDefect, invalid.Branch)
	require.Zero(t, calls.Load())
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		status.Store(int32(code))
		_, err := sdkgo.RunQuery(queryContext{}, client.GetPost(), testConnection, GetPostInput{PostID: "123456789_987654321"})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testPageToken)
	}
	status.Store(http.StatusNotFound)
	missing, err := sdkgo.RunQuery(queryContext{}, client.GetPost(), testConnection, GetPostInput{PostID: "123456789_987654321"})
	require.NoError(t, err)
	require.Equal(t, GetPostBranchNotFound, missing.Branch)
	status.Store(http.StatusForbidden)
	rejected, err := sdkgo.RunQuery(queryContext{}, client.ListPosts(), testConnection, ListPostsInput{PageID: "123456789"})
	require.NoError(t, err)
	require.Equal(t, ListPostsBranchProviderRejected, rejected.Branch)
}

func TestGraphRejectsMalformedPosts(t *testing.T) {
	var body atomic.Value
	body.Store(`{"data":{}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(body.Load().(string)))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	page, err := sdkgo.RunQuery(queryContext{}, client.ListPosts(), testConnection, ListPostsInput{PageID: "123456789"})
	require.NoError(t, err)
	require.Equal(t, ListPostsBranchInvalidResponse, page.Branch)
	body.Store(`{"id":"bad/id","created_time":"2026-11-08T10:00:00+0000"}`)
	post, err := sdkgo.RunQuery(queryContext{}, client.GetPost(), testConnection, GetPostInput{PostID: "123456789_987654321"})
	require.NoError(t, err)
	require.Equal(t, GetPostBranchInvalidResponse, post.Branch)
}
