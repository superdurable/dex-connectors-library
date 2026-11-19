// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spotify_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/spotify/go"
	"github.com/superdurable/dex-connectors-library/connectors/spotify/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetPlaylistReadsMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v1/playlists/"+testPlaylistID, request.URL.Path)
		require.Equal(t, "Bearer one-use-token", request.Header.Get("Authorization"))
		writeJSON(t, response, map[string]any{
			"id": testPlaylistID, "name": "Example Playlist", "description": "Example",
			"owner":         map[string]any{"id": "sample-user"},
			"external_urls": map[string]any{"spotify": "https://open.spotify.com/playlist/" + testPlaylistID},
			"snapshot_id":   "sample-snapshot", "public": true,
		})
	}))
	defer server.Close()
	client := newClient(t, server.URL+"/v1", spotify.Config{})
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("playlist-flow", "get-playlist"),
		client.GetPlaylist(), spotifyConnection, spotify.GetPlaylistInput{PlaylistID: testPlaylistID})
	require.NoError(t, err)
	require.Equal(t, spotify.GetPlaylistBranchFound, result.Branch)
	require.Equal(t, "Example Playlist", result.Value.Name)
	require.Equal(t, "sample-user", result.Value.OwnerUserID)
	require.Equal(t, "https://open.spotify.com/playlist/"+testPlaylistID, result.Value.SpotifyURL)
	require.NotNil(t, result.Value.Public)
	require.True(t, *result.Value.Public)
}

func TestGetPlaylistClassifiesProviderOutcomes(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"revoked", 401, `{"error":"secret-token"}`, spotify.GetPlaylistBranchAuthorizationRevoked, false},
		{"denied", 403, `{}`, spotify.GetPlaylistBranchAccessDenied, false},
		{"missing", 404, `{}`, spotify.GetPlaylistBranchNotFound, false},
		{"rejected", 400, `{}`, spotify.GetPlaylistBranchProviderRejected, false},
		{"malformed", 200, `{`, spotify.GetPlaylistBranchInvalidResponse, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(test.status)
				_, err := response.Write([]byte(test.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			client := newClient(t, server.URL+"/v1", spotify.Config{})
			result, err := sdkgo.RunQuery(testsupport.NewDexContext("playlist-flow", "get-playlist"),
				client.GetPlaylist(), spotifyConnection, spotify.GetPlaylistInput{PlaylistID: testPlaylistID})
			if test.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.NotContains(t, result.Failure.Message, "secret-token")
		})
	}
}
