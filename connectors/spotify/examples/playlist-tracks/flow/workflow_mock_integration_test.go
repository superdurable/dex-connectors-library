//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package playlisttracks

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/spotify"
	"github.com/superdurable/dex-connectors-library/connectors/spotify/spotifymock"
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

	calls := mock.ListPlaylistTracks().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, spotify.ListPlaylistTracksInput{PlaylistID: "3cEYpjA9oz9GiPac4AsH4n", Limit: 2}, calls[0].Input)
	require.Equal(t, spotify.ListPlaylistTracksBranchListed, calls[0].Branch)
}
