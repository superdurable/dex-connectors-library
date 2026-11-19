//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package playlisttracks

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/spotify/go"
	"github.com/superdurable/dex-connectors-library/connectors/spotify/go/spotifymock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestPlaylistTracksUsesMockWithRealDex(t *testing.T) {
	mock := spotifymock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	client := startPlaylistWorker(t, flow)
	input := Input{PlaylistID: "3cEYpjA9oz9GiPac4AsH4n", Limit: 2}
	result := runPlaylistFlow(t, client, flow, fmt.Sprintf("spotify-playlist-mock-%d", time.Now().UnixNano()), input)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow failed: %s", result.ErrorMessage)

	var page spotify.PlaylistTrackPage
	require.NoError(t, result.DecodeSingleOutput(&page))
	require.Len(t, page.Tracks, 1)
	require.Equal(t, 0, page.Tracks[0].Position)

	playlistCalls := mock.GetPlaylist().Calls()
	require.Len(t, playlistCalls, 1)
	require.Equal(t, spotify.GetPlaylistInput{PlaylistID: input.PlaylistID}, playlistCalls[0].Input)
	require.Equal(t, spotify.GetPlaylistBranchFound, playlistCalls[0].Branch)

	calls := mock.ListPlaylistTracks().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, spotify.ListPlaylistTracksInput{PlaylistID: "3cEYpjA9oz9GiPac4AsH4n", Limit: 2}, calls[0].Input)
	require.Equal(t, spotify.ListPlaylistTracksBranchListed, calls[0].Branch)

	albumCalls := mock.GetAlbum().Calls()
	require.Len(t, albumCalls, 1)
	require.Equal(t, spotify.GetAlbumInput{AlbumID: page.Tracks[0].Album.ID}, albumCalls[0].Input)
	require.Equal(t, spotify.GetAlbumBranchFound, albumCalls[0].Branch)

	artistCalls := mock.GetArtist().Calls()
	require.Len(t, artistCalls, 1)
	require.Equal(t, spotify.GetArtistInput{ArtistID: "0OdUWJ0sBjDrqHygGUXeCF"}, artistCalls[0].Input)
	require.Equal(t, spotify.GetArtistBranchFound, artistCalls[0].Branch)

	trackCalls := mock.GetTrack().Calls()
	require.Len(t, trackCalls, 1)
	require.Equal(t, spotify.GetTrackInput{TrackID: page.Tracks[0].ID}, trackCalls[0].Input)
	require.Equal(t, spotify.GetTrackBranchFound, trackCalls[0].Branch)
}
