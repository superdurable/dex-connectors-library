// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wordpress

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "wordpress-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "wordpress", Name: "wordpress"}

const publishedPost = `{"id":42,"slug":"meridian-release","status":"publish","link":"https://example.test/meridian-release/","title":{"rendered":"Meridian release"},"content":{"rendered":"<p>Released.</p>"},"date_gmt":"2026-11-07T12:00:00"}`

func newTestClient(t *testing.T, siteURL string) *Client {
	t.Helper()
	client, err := New(Config{SiteURL: siteURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: Credentials{Username: "editor", ApplicationPassword: sdkgo.NewSecretString("abcd efgh")}})
	require.NoError(t, err)
	return client
}

func TestGetPostProviderOutcomes(t *testing.T) {
	status, body, requests := http.StatusOK, publishedPost, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		username, password, hasBasic := r.BasicAuth()
		require.True(t, hasBasic)
		require.Equal(t, "editor", username)
		require.Equal(t, "abcdefgh", password)
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/wp-json/wp/v2/posts/42", r.URL.Path)
		w.WriteHeader(status)
		_, err := fmt.Fprint(w, body)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	found, err := sdkgo.RunQuery(queryContext{}, client.GetPost(), testConnection, GetPostInput{PostID: 42})
	require.NoError(t, err)
	require.Equal(t, GetPostBranchFound, found.Branch)
	require.Equal(t, "<p>Released.</p>", found.Value.Content.Rendered)
	body = `{"id":42,"status":"draft"}`
	draft, err := sdkgo.RunQuery(queryContext{}, client.GetPost(), testConnection, GetPostInput{PostID: 42})
	require.NoError(t, err)
	require.Equal(t, GetPostBranchNotFound, draft.Branch)
	status = http.StatusNotFound
	missing, err := sdkgo.RunQuery(queryContext{}, client.GetPost(), testConnection, GetPostInput{PostID: 42})
	require.NoError(t, err)
	require.Equal(t, GetPostBranchNotFound, missing.Branch)
	status = http.StatusUnauthorized
	rejected, err := sdkgo.RunQuery(queryContext{}, client.GetPost(), testConnection, GetPostInput{PostID: 42})
	require.NoError(t, err)
	require.Equal(t, GetPostBranchProviderRejected, rejected.Branch)
	status = http.StatusTooManyRequests
	_, err = sdkgo.RunQuery(queryContext{}, client.GetPost(), testConnection, GetPostInput{PostID: 42})
	require.Error(t, err)
	invalid, err := sdkgo.RunQuery(queryContext{}, client.GetPost(), testConnection, GetPostInput{})
	require.NoError(t, err)
	require.Equal(t, GetPostBranchDefect, invalid.Branch)
	require.Equal(t, 5, requests)
}

func TestListPostsPaginationAndMalformedResponse(t *testing.T) {
	body, requests := "["+publishedPost+"]", 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, "/blog/wp-json/wp/v2/posts", r.URL.Path)
		require.Equal(t, "publish", r.URL.Query().Get("status"))
		require.Equal(t, "2", r.URL.Query().Get("page"))
		require.Equal(t, "20", r.URL.Query().Get("per_page"))
		w.Header().Set("X-WP-Total", "21")
		w.Header().Set("X-WP-TotalPages", "2")
		_, err := fmt.Fprint(w, body)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/blog")
	input := ListPostsInput{Page: 2, PerPage: 20, After: "2026-11-01T00:00:00Z"}
	listed, err := sdkgo.RunQuery(queryContext{}, client.ListPosts(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, ListPostsBranchListed, listed.Branch)
	require.Len(t, listed.Value.Posts, 1)
	require.Equal(t, 21, listed.Value.Total)
	body = `[]`
	empty, err := sdkgo.RunQuery(queryContext{}, client.ListPosts(), testConnection, input)
	require.NoError(t, err)
	require.Empty(t, empty.Value.Posts)
	body = `{}`
	malformed, err := sdkgo.RunQuery(queryContext{}, client.ListPosts(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, ListPostsBranchInvalidResponse, malformed.Branch)
	require.Equal(t, 3, requests)
}
