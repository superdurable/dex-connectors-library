// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package facebookpages

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestPublishTextPostSingleDispatch(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/123456789/feed", r.URL.Path)
		require.Equal(t, "Bearer "+testPageToken, r.Header.Get("Authorization"))
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		require.Equal(t, "Hello from Dex", r.PostForm.Get("message"))
		require.Empty(t, r.URL.Query().Get("access_token"))
		_, err := fmt.Fprint(w, `{"id":"123456789_987654321"}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("facebook-pages-publish", "publish-post")
	input := PublishTextPostInput{PageID: "123456789", Message: "Hello from Dex"}
	published, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, PublishTextPostBranchPublished, published.Branch)
	require.Equal(t, "123456789_987654321", published.Value.PostID)
	ctx.AdvanceAttempt()
	uncertain, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, PublishTextPostBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, requests)
}

func TestPublishTextPostRetriesDefinitiveRateLimitAndUnavailable(t *testing.T) {
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			status, requests := code, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests++
				w.WriteHeader(status)
				if status == http.StatusOK {
					_, err := fmt.Fprint(w, `{"id":"123456789_987654321"}`)
					require.NoError(t, err)
				}
			}))
			defer server.Close()
			ctx := singledispatchtest.NewContext("facebook-pages-retry", "publish-post")
			client := newTestClient(t, server.URL)
			input := PublishTextPostInput{PageID: "123456789", Message: "Hello"}
			_, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, input)
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			status = http.StatusOK
			ctx.AdvanceAttempt()
			result, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, input)
			require.NoError(t, err)
			require.Equal(t, PublishTextPostBranchPublished, result.Branch)
			require.Equal(t, 2, requests)
		})
	}
}

func TestPublishTextPostAmbiguousResponseDoesNotResend(t *testing.T) {
	for _, response := range []struct {
		status int
		body   string
	}{{500, ""}, {200, `{}`}} {
		requests := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests++
			w.WriteHeader(response.status)
			_, err := fmt.Fprint(w, response.body)
			require.NoError(t, err)
		}))
		client := newTestClient(t, server.URL)
		ctx := singledispatchtest.NewContext("facebook-pages-uncertain", "publish-post")
		input := PublishTextPostInput{PageID: "123456789", Message: "Hello"}
		first, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, PublishTextPostBranchUncertain, first.Branch)
		ctx.AdvanceAttempt()
		second, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, PublishTextPostBranchUncertain, second.Branch)
		require.Equal(t, 1, requests)
		server.Close()
	}
}

func TestPublishTextPostInterruptedConnectionIsUncertain(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.NoError(t, r.ParseForm())
		require.Equal(t, "Hello", r.PostForm.Get("message"))
		connection, _, err := w.(http.Hijacker).Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("facebook-pages-interrupted", "publish-post")
	input := PublishTextPostInput{PageID: "123456789", Message: "Hello"}
	for range 2 {
		result, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, PublishTextPostBranchUncertain, result.Branch)
		ctx.AdvanceAttempt()
	}
	require.Equal(t, 1, requests)
}

func TestPublishTextPostRejectsInvalidInputAndProviderFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, err := fmt.Fprint(w, `{"error":{"message":"private user data"}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("facebook-pages-reject", "publish-post")
	invalid, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, PublishTextPostInput{PageID: "bad/id", Message: "Hello"})
	require.NoError(t, err)
	require.Equal(t, PublishTextPostBranchDefect, invalid.Branch)
	rejected, err := sdkgo.RunMutation(ctx, client.PublishTextPost(), testConnection, PublishTextPostInput{PageID: "123456789", Message: "Hello"})
	require.NoError(t, err)
	require.Equal(t, PublishTextPostBranchProviderRejected, rejected.Branch)
	require.NotContains(t, rejected.Failure.Message, "private user data")
	require.NotContains(t, rejected.Failure.Message, testPageToken)
}
