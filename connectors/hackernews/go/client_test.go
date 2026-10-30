// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hackernews

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

func (queryContext) FlowID() string              { return "provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "hacker-news", Name: "public"}

func TestFeedSnapshotPreservesOrderAndBounds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/showstories.json", r.URL.Path)
		require.Empty(t, r.Header.Get("Authorization"))
		_, err := fmt.Fprint(w, `[9,3,7]`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, nil)
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListStoryIDs(), testConnection, ListStoryIDsInput{Feed: "show", Limit: 2})
	require.NoError(t, err)
	require.Equal(t, ListStoryIDsBranchListed, result.Branch)
	require.Equal(t, []int64{9, 3}, result.Value.IDs)
	require.True(t, result.Value.HasMore)
	require.WithinDuration(t, time.Now(), result.Value.ObservedAt, time.Second)
}

func TestItemResponseBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, body string
		branch     sdkgo.BranchID
	}{
		{"story", `{"id":42,"type":"story","title":"A &amp; B","url":"https://example.com/release","kids":[43],"score":10}`, GetItemBranchFound},
		{"comment", `{"id":42,"type":"comment","parent":41,"text":"<p>Evidence</p>"}`, GetItemBranchFound},
		{"missing", `null`, GetItemBranchNotFound},
		{"deleted", `{"id":42,"deleted":true}`, GetItemBranchNotFound},
		{"dead", `{"id":42,"dead":true}`, GetItemBranchNotFound},
		{"wrong identity", `{"id":41,"type":"story"}`, GetItemBranchInvalidResponse},
		{"unknown type", `{"id":42,"type":"unknown"}`, GetItemBranchInvalidResponse},
		{"bad child", `{"id":42,"type":"story","kids":[0]}`, GetItemBranchInvalidResponse},
		{"bad JSON", `{`, GetItemBranchInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/item/42.json", r.URL.Path)
				_, err := fmt.Fprint(w, test.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			client, err := New(Config{BaseURL: server.URL}, nil)
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(queryContext{}, client.GetItem(), testConnection, GetItemInput{ID: 42})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			if result.Branch == GetItemBranchFound {
				require.Equal(t, "https://news.ycombinator.com/item?id=42", result.Value.DiscussionURL)
			}
		})
	}
}

func TestHTTPFailureClassification(t *testing.T) {
	for _, test := range []struct {
		status  int
		branch  sdkgo.BranchID
		isRetry bool
	}{
		{429, "", true}, {503, "", true}, {408, "", true}, {404, GetItemBranchNotFound, false},
		{403, GetItemBranchProviderRejected, false}, {302, GetItemBranchProviderRejected, false},
	} {
		t.Run(fmt.Sprint(test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "2")
				w.Header().Set("Location", "/unexpected")
				w.WriteHeader(test.status)
			}))
			defer server.Close()
			client, err := New(Config{BaseURL: server.URL}, nil)
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(queryContext{}, client.GetItem(), testConnection, GetItemInput{ID: 42})
			if test.isRetry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.branch, result.Branch)
			}
		})
	}
}

func TestInvalidInputDoesNotCallProvider(t *testing.T) {
	client, err := New(Config{}, nil)
	require.NoError(t, err)
	for _, input := range []ListStoryIDsInput{{Feed: "../item/1"}, {Limit: -1}, {Limit: 501}} {
		result, err := sdkgo.RunQuery(queryContext{}, client.ListStoryIDs(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, ListStoryIDsBranchDefect, result.Branch)
	}
	result, err := sdkgo.RunQuery(queryContext{}, client.GetItem(), testConnection, GetItemInput{})
	require.NoError(t, err)
	require.Equal(t, GetItemBranchDefect, result.Branch)
}

func TestFeedRejectsMalformedAndOversizedResponses(t *testing.T) {
	for _, body := range []string{`null`, `[0]`, `[-1]`, `{}`, `[1,2,3,4,5]`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, err := fmt.Fprint(w, body); require.NoError(t, err) }))
			defer server.Close()
			client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 10}, nil)
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(queryContext{}, client.ListStoryIDs(), testConnection, ListStoryIDsInput{})
			require.NoError(t, err)
			require.Equal(t, ListStoryIDsBranchInvalidResponse, result.Branch)
		})
	}
}
