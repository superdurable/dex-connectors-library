// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shortcut

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const storyResponse = `{"id":12345,"name":"Example story","story_type":"feature","workflow_state_id":678,"archived":false}`

func TestSearchStoriesReadsBoundedPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/search/stories", request.URL.Path)
		require.Equal(t, "release", request.URL.Query().Get("query"))
		require.Equal(t, "slim", request.URL.Query().Get("detail"))
		_, err := writer.Write([]byte(`{"data":[` + storyResponse + `],"next":"cursor","total":1}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).SearchStories(), testConnection, SearchStoriesInput{Query: "release"})
	require.NoError(t, err)
	require.Equal(t, SearchStoriesBranchFound, result.Branch)
	require.Equal(t, "12345", result.Value.Stories[0].StoryID)
	require.Equal(t, "cursor", result.Value.Next)
}

func TestCreateStorySendsOneFencedRequest(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestCount++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/stories", request.URL.Path)
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		writer.WriteHeader(http.StatusCreated)
		_, err := writer.Write([]byte(storyResponse))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL, 0).CreateStory(), testConnection,
		CreateStoryInput{Name: "Example story", StoryType: "feature", WorkflowStateID: "678"})
	require.NoError(t, err)
	require.Equal(t, CreateStoryBranchCreated, result.Branch)
	require.Equal(t, "12345", result.Value.StoryID)
	require.Equal(t, 1, requestCount)
}

func TestUpdateStorySetsAbsoluteFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPut, request.Method)
		require.Equal(t, "/stories/12345", request.URL.Path)
		_, err := writer.Write([]byte(storyResponse))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL, 0).UpdateStory(), testConnection,
		UpdateStoryInput{StoryID: "12345", Name: "Example story"})
	require.NoError(t, err)
	require.Equal(t, UpdateStoryBranchUpdated, result.Branch)
}

func TestDeleteStoryAcceptsNoContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodDelete, request.Method)
		require.Equal(t, "/stories/12345", request.URL.Path)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL, 0).DeleteStory(), testConnection,
		DeleteStoryInput{StoryID: "12345"})
	require.NoError(t, err)
	require.Equal(t, DeleteStoryBranchDeleted, result.Branch)
	require.Equal(t, "12345", result.Value.StoryID)
}

func TestStoryOperationsRetryTransientResponses(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(status)
		}))
		client := newTestClient(t, server.URL, 0)
		_, searchErr := sdkgo.RunQuery(queryContext{}, client.SearchStories(), testConnection, SearchStoriesInput{Query: "release"})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, searchErr, &retry)
		_, updateErr := sdkgo.RunMutation(queryContext{}, client.UpdateStory(), testConnection, UpdateStoryInput{StoryID: "12345", Name: "Example story"})
		require.ErrorAs(t, updateErr, &retry)
		_, deleteErr := sdkgo.RunMutation(queryContext{}, client.DeleteStory(), testConnection, DeleteStoryInput{StoryID: "12345"})
		require.ErrorAs(t, deleteErr, &retry)
		server.Close()
	}
}
