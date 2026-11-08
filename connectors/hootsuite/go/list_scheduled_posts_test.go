// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hootsuite

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListScheduledPostsProjectsOneProviderPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/messages", r.URL.Path)
		require.Equal(t, "SCHEDULED", r.URL.Query().Get("state"))
		require.Equal(t, "2026-11-15T14:00:00Z", r.URL.Query().Get("startTime"))
		require.Equal(t, "2026-11-20T14:00:00Z", r.URL.Query().Get("endTime"))
		require.Equal(t, "115185509", r.URL.Query().Get("socialProfileIds"))
		require.Equal(t, "cursor/+value", r.URL.Query().Get("cursor"))
		_, err := w.Write([]byte(`{"data":[{"id":"4138476039","state":"SCHEDULED","text":"private draft","socialProfile":{"id":"115185509"},"scheduledSendTime":"2026-11-18T14:00:00Z"}],"metadata":{"cursor":{"next":"next:123"}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	input := ListScheduledPostsInput{StartTime: "2026-11-15T14:00:00Z", EndTime: "2026-11-20T14:00:00Z", SocialProfileID: "115185509", Cursor: "cursor/+value"}
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListScheduledPosts(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, ListScheduledPostsBranchListed, result.Branch)
	require.Equal(t, "4138476039", result.Value.Posts[0].ID)
	require.Equal(t, "next:123", result.Value.NextCursor)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private draft")
}

func TestListScheduledPostsRejectsLongRangeBeforeRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("unexpected provider call") }))
	defer server.Close()
	input := ListScheduledPostsInput{StartTime: "2026-11-01T00:00:00Z", EndTime: "2026-12-01T00:00:00Z"}
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListScheduledPosts(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, ListScheduledPostsBranchDefect, result.Branch)
}
