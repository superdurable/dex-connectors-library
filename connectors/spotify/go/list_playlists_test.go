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

func TestListUserPlaylistsAndSearchPlaylistsUseDocumentedEndpoints(t *testing.T) {
	playlistPage := map[string]any{
		"limit": 1, "offset": 0, "total": 2,
		"items": []any{map[string]any{
			"id": testPlaylistID, "name": "Example Playlist", "description": "Example",
			"owner":         map[string]any{"id": "sample-user"},
			"external_urls": map[string]any{"spotify": "https://open.spotify.com/playlist/" + testPlaylistID},
			"snapshot_id":   "sample-snapshot", "public": true,
		}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer one-use-token", request.Header.Get("Authorization"))
		require.Equal(t, "1", request.URL.Query().Get("limit"))
		require.Equal(t, "0", request.URL.Query().Get("offset"))
		switch request.URL.Path {
		case "/v1/users/sample-user/playlists":
			writeJSON(t, response, playlistPage)
		case "/v1/search":
			require.Equal(t, "Example", request.URL.Query().Get("q"))
			require.Equal(t, "playlist", request.URL.Query().Get("type"))
			require.Equal(t, "US", request.URL.Query().Get("market"))
			writeJSON(t, response, map[string]any{"playlists": playlistPage})
		default:
			t.Errorf("unexpected Spotify path %s", request.URL.Path)
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := newClient(t, server.URL+"/v1", spotify.Config{})
	listed, err := sdkgo.RunQuery(testsupport.NewDexContext("playlist-flow", "list-user-playlists"),
		client.ListUserPlaylists(), spotifyConnection,
		spotify.ListUserPlaylistsInput{UserID: "sample-user", Limit: 1})
	require.NoError(t, err)
	require.Equal(t, spotify.ListUserPlaylistsBranchListed, listed.Branch)
	require.Equal(t, 1, listed.Value.NextOffset)
	require.Equal(t, "Example Playlist", listed.Value.Playlists[0].Name)
	searched, err := sdkgo.RunQuery(testsupport.NewDexContext("playlist-flow", "search-playlists"),
		client.SearchPlaylists(), spotifyConnection,
		spotify.SearchPlaylistsInput{Query: " Example ", Market: "us", Limit: 1})
	require.NoError(t, err)
	require.Equal(t, spotify.SearchPlaylistsBranchListed, searched.Branch)
	require.Equal(t, listed.Value, searched.Value)
}
