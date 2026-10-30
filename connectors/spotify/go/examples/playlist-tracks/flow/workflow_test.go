// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package playlisttracks

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/spotify"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestMapToListPlaylistTracksInputPreservesThePageRequest(t *testing.T) {
	input := Input{PlaylistID: "3cEYpjA9oz9GiPac4AsH4n", Market: "US", Limit: 25, Offset: 50}
	require.Equal(t, spotify.ListPlaylistTracksInput{
		PlaylistID: input.PlaylistID, Market: "US", Limit: 25, Offset: 50,
	}, NewFlow(spotify.Connection{}).MapToListPlaylistTracksInput(input))
}

// TestStartFlowIdentitiesMatchTheFlowDefinition keeps registered types equal to the dexcli visualize names.
func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(spotify.Connection{})))
	require.Equal(t, recordPlaylistRequestStepType, dex.GetFinalStepType[Input](recordPlaylistRequest{}))
	require.Equal(t, completePlaylistTracksStepType, dex.GetFinalStepType[spotify.ListPlaylistTracksResult](completePlaylistTracks{}))
	wait, err := recordPlaylistRequest{}.WaitFor(nil, Input{PlaylistID: "3cEYpjA9oz9GiPac4AsH4n"})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	client, err := spotify.New(spotify.Config{}, sdkgo.StaticCredentialProvider[spotify.Credentials]{})
	require.NoError(t, err)
	connection, err := spotify.NewConnection(client, sdkgo.ConnectionRef{Provider: "spotify", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)

	otherConnection, err := spotify.NewConnection(client, sdkgo.ConnectionRef{Provider: "spotify", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection)}) })
}
