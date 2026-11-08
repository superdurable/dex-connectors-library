// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wordpress

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestCreatePostConfirmsDraftOnce(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		username, password, hasBasic := r.BasicAuth()
		require.True(t, hasBasic)
		require.Equal(t, "editor", username)
		require.Equal(t, "abcdefgh", password)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/wp-json/wp/v2/posts", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var body struct {
			Title   string `json:"title"`
			Content string `json:"content"`
			Status  string `json:"status"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "Meridian release", body.Title)
		require.Equal(t, "<p>Draft.</p>", body.Content)
		require.Equal(t, "draft", body.Status)
		w.WriteHeader(http.StatusCreated)
		_, err := fmt.Fprint(w, `{"id":43,"status":"draft"}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("create-post-flow", "create-post-step")
	input := CreatePostInput{Title: "Meridian release", Content: "<p>Draft.</p>"}
	created, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreatePostBranchCreated, created.Branch)
	require.EqualValues(t, 43, created.Value.ID)
	ctx.AdvanceAttempt()
	uncertain, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreatePostBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, requests)
}

func TestCreatePostRetries429ThenCreates(t *testing.T) {
	status, requests := http.StatusTooManyRequests, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(status)
		if status == http.StatusCreated {
			_, err := fmt.Fprint(w, `{"id":44,"status":"publish"}`)
			require.NoError(t, err)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("rate-limit-flow", "create-post-step")
	input := CreatePostInput{Title: "Meridian release", Status: "publish"}
	_, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, input)
	require.Error(t, err)
	status = http.StatusCreated
	ctx.AdvanceAttempt()
	created, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreatePostBranchCreated, created.Branch)
	require.Equal(t, "publish", created.Value.Status)
	require.Equal(t, 2, requests)
}

func TestCreatePostDoesNotRepeatAmbiguousServerError(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("uncertain-flow", "create-post-step")
	input := CreatePostInput{Title: "Meridian release"}
	uncertain, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreatePostBranchUncertain, uncertain.Branch)
	ctx.AdvanceAttempt()
	repeated, err := sdkgo.RunMutation(ctx, client.CreatePost(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreatePostBranchUncertain, repeated.Branch)
	require.Equal(t, 1, requests)
}
