//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spotify_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/spotify"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestLiveListPlaylistTracks(t *testing.T) {
	token := os.Getenv("SPOTIFY_CONNECTOR_TEST_TOKEN")
	playlistID := os.Getenv("SPOTIFY_CONNECTOR_TEST_PLAYLIST_ID")
	if token == "" || playlistID == "" {
		t.Skip("SPOTIFY_CONNECTOR_TEST_TOKEN and SPOTIFY_CONNECTOR_TEST_PLAYLIST_ID are not configured")
	}
	client, err := spotify.New(spotify.Config{}, sdkgo.StaticCredentialProvider[spotify.Credentials]{
		spotifyConnection: {AccessToken: sdkgo.NewSecretString(token)},
	})
	require.NoError(t, err)
	result, err := runList(t, client, spotify.ListPlaylistTracksInput{PlaylistID: playlistID, Limit: 1})
	require.NoError(t, err)
	require.Equal(t, spotify.ListPlaylistTracksBranchListed, result.Branch)
	require.Equal(t, playlistID, result.Value.PlaylistID)
}
