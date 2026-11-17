// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package facebookpages

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestUpdatePostReplacesMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/123456789_987654321", request.URL.Path)
		require.Equal(t, "Bearer "+testPageToken, request.Header.Get("Authorization"))
		require.NoError(t, request.ParseForm())
		require.Equal(t, "Updated from Dex", request.PostForm.Get("message"))
		_, err := writer.Write([]byte(`{"success":true}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL).UpdatePost(), testConnection, UpdatePostInput{PostID: "123456789_987654321", Message: "Updated from Dex"})
	require.NoError(t, err)
	require.Equal(t, UpdatePostBranchUpdated, result.Branch)
	require.Equal(t, "123456789_987654321", result.Value.PostID)
}

func TestDeletePostAcceptsEmptySuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodDelete, request.Method)
		require.Equal(t, "/123456789_987654321", request.URL.Path)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL).DeletePost(), testConnection, DeletePostInput{PostID: "123456789_987654321"})
	require.NoError(t, err)
	require.Equal(t, DeletePostBranchDeleted, result.Branch)
}

func TestPostChangesClassifyProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		status int
		branch sdkgo.BranchID
		retry  bool
	}{{404, UpdatePostBranchNotFound, false}, {403, UpdatePostBranchProviderRejected, false}, {200, UpdatePostBranchInvalidResponse, false}, {429, "", true}, {503, "", true}, {500, "", true}} {
		t.Run(http.StatusText(testCase.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(`{}`))
				require.NoError(t, err)
			}))
			defer server.Close()
			client := newTestClient(t, server.URL)
			updated, err := sdkgo.RunMutation(queryContext{}, client.UpdatePost(), testConnection, UpdatePostInput{PostID: "123456789_987654321", Message: "Updated"})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
			} else {
				require.NoError(t, err)
				require.Equal(t, testCase.branch, updated.Branch)
			}
		})
	}
}

func TestPostChangesRejectInvalidIDs(t *testing.T) {
	client := newTestClient(t, "https://graph.facebook.com/v25.0")
	updated, err := sdkgo.RunMutation(queryContext{}, client.UpdatePost(), testConnection, UpdatePostInput{PostID: "../bad", Message: "Updated"})
	require.NoError(t, err)
	require.Equal(t, UpdatePostBranchDefect, updated.Branch)
	deleted, err := sdkgo.RunMutation(queryContext{}, client.DeletePost(), testConnection, DeletePostInput{PostID: "../bad"})
	require.NoError(t, err)
	require.Equal(t, DeletePostBranchDefect, deleted.Branch)
}
