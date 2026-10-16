// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package playlistcurator

import (
	"testing"

	"github.com/stretchr/testify/require"
	youtube "github.com/superdurable/dex-connectors-library/connectors/google/youtube"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestPrepareCurationRequestTrimsAndValidates(t *testing.T) {
	prepared, err := PrepareCurationRequest(CurationRequest{Channel: " @GoogleDevelopers ", Query: " release ", PlaylistID: " PLx ", ChannelID: "forged"})
	require.NoError(t, err)
	require.Equal(t, CurationRequest{Channel: "@GoogleDevelopers", Query: "release", PlaylistID: "PLx"}, prepared)
	for name, request := range map[string]CurationRequest{
		"blank channel":  {Query: "q", PlaylistID: "PLx"},
		"blank query":    {Channel: "@a", PlaylistID: "PLx"},
		"long query":     {Channel: "@a", Query: string(make([]rune, 201)), PlaylistID: "PLx"},
		"blank playlist": {Channel: "@a", Query: "q"},
	} {
		_, err := PrepareCurationRequest(request)
		require.Error(t, err, name)
	}
}

func TestMappers(t *testing.T) {
	require.Equal(t, youtube.GetChannelInput{ChannelID: "UC_x5XG1OV2P6uZZ5FSM9Ttw"}, MapToGetChannelInput(CurationRequest{Channel: "UC_x5XG1OV2P6uZZ5FSM9Ttw"}))
	require.Equal(t, youtube.GetChannelInput{Handle: "@GoogleDevelopers"}, MapToGetChannelInput(CurationRequest{Channel: "@GoogleDevelopers"}))
	require.Equal(t, youtube.SearchVideosInput{Query: "release", ChannelID: "UC_x5XG1OV2P6uZZ5FSM9Ttw", Order: "date", MaxResults: 1},
		MapToSearchVideosInput(CurationRequest{Query: "release", ChannelID: "UC_x5XG1OV2P6uZZ5FSM9Ttw"}))
}

// TestStartFlowIdentitiesMatchTheFlowDefinition keeps registered types equal to the dexcli visualize names that Start Flow sends.
func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, "YouTubePlaylistCurator", dex.GetFinalFlowType(NewFlow(youtube.Connection{}, youtube.Connection{})))
	require.Equal(t, "RecordCurationRequest", dex.GetFinalStepType[CurationRequest](recordCurationRequest{}))
	wait, err := recordCurationRequest{}.WaitFor(nil, CurationRequest{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithTheStaticConnectionNames(t *testing.T) {
	connection := func(name string, method string) youtube.Connection {
		reference := sdkgo.ConnectionRef{Provider: "google", Name: name}
		client, err := youtube.New(youtube.Config{}, sdkgo.StaticCredentialProvider[youtube.Credentials]{
			reference: {AuthMethodID: method, AccessToken: sdkgo.NewSecretString("token")},
		})
		require.NoError(t, err)
		bound, err := youtube.NewConnection(client, reference)
		require.NoError(t, err)
		return bound
	}
	read := connection(ReadConnectionName, youtube.ReadOnlyAuthMethodID)
	manage := connection(ManageConnectionName, youtube.ManageAuthMethodID)
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(read, manage)})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(manage, read)}) }, "swapped connections must not register")
}
