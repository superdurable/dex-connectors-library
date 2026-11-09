// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shortcut

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

var testConnection = sdkgo.ConnectionRef{Provider: "shortcut", Name: "shortcut"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "shortcut-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-story" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{APIToken: sdkgo.NewSecretString("sample-shortcut-token")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetStoryReadsSelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/stories/12345", request.URL.Path)
		require.Equal(t, "sample-shortcut-token", request.Header.Get("Shortcut-Token"))
		_, err := writer.Write([]byte(`{"id":12345,"name":"Example story","story_type":"feature","workflow_state_id":678,"archived":false,"description":"private details","comments":[{"text":"not returned"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetStory(), testConnection, GetStoryInput{StoryID: "12345"})
	require.NoError(t, err)
	require.Equal(t, GetStoryBranchFound, result.Branch)
	require.Equal(t, StoryMetadata{StoryID: "12345", Name: "Example story", StoryType: "feature", WorkflowStateID: "678"}, result.Value)
}

func TestGetStoryClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: GetStoryBranchNotFound},
		{name: "forbidden", status: 403, branch: GetStoryBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"id":12345,"name":"Example story"}`, branch: GetStoryBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetStory(), testConnection, GetStoryInput{StoryID: "12345"})
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

func TestGetStoryRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://api.app.shortcut.com/api/v3", 0)
	for _, storyID := range []string{"", "0", "01", "../other", "9007199254740992"} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetStory(), testConnection, GetStoryInput{StoryID: storyID})
		require.NoError(t, err)
		require.Equal(t, GetStoryBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"id":12345,"name":"Example story","story_type":"feature","workflow_state_id":678,"archived":false}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 20).GetStory(), testConnection, GetStoryInput{StoryID: "12345"})
	require.NoError(t, err)
	require.Equal(t, GetStoryBranchInvalidResponse, result.Branch)
}
