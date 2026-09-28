// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spotify_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/spotify"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type listedTarget struct {
	dex.StepDefaultsNoWaitFor[spotify.ListPlaylistTracksResult]
}

func (listedTarget) Execute(dex.Context, spotify.ListPlaylistTracksResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

func TestListPlaylistTracksFactoryAppliesManifestDefaults(t *testing.T) {
	step := spotify.NewListPlaylistTracksStep(spotify.ListPlaylistTracksStepConfig[string]{
		StepType: "ListTracks", ConnectionName: spotifyConnection.Name,
		Annotations: sdkgo.StepAnnotations{GroupID: "spotify", GroupLabel: "Spotify", Explanation: "List one playlist page."},
		Connection:  factoryConnection(t), MapToOperationInput: func(string) spotify.ListPlaylistTracksInput {
			return spotify.ListPlaylistTracksInput{PlaylistID: testPlaylistID}
		},
		Listed: sdkgo.GoTo(listedTarget{}),
	})
	require.Equal(t, "ListTracks", step.GetStepType())
	options := step.GetStepOptions()
	require.Equal(t, dex.StepDurabilityAsync, options.ExecuteDurability)
	require.Equal(t, 30*time.Second, options.ExecuteMethodTimeout)
	require.Equal(t, &dex.RetryPolicy{
		InitialInterval: time.Second, BackoffCoefficient: 2, MaximumInterval: 30 * time.Second,
		MaximumAttempts: 5, TotalDuration: 2 * time.Minute,
	}, options.ExecuteRetry)
}

func TestListPlaylistTracksFactoryFailsClosed(t *testing.T) {
	connection := factoryConnection(t)
	require.Panics(t, func() {
		spotify.NewListPlaylistTracksStep(spotify.ListPlaylistTracksStepConfig[string]{
			StepType: "ListTracks", Connection: connection,
			MapToOperationInput: func(string) spotify.ListPlaylistTracksInput { return spotify.ListPlaylistTracksInput{} },
		})
	}, "the listed branch is required")
	require.Panics(t, func() {
		spotify.NewListPlaylistTracksStep(spotify.ListPlaylistTracksStepConfig[string]{
			StepType: "ListTracks", ConnectionName: "another-connection", Connection: connection,
			MapToOperationInput: func(string) spotify.ListPlaylistTracksInput { return spotify.ListPlaylistTracksInput{} },
			Listed:              sdkgo.GoTo(listedTarget{}),
		})
	}, "a static connection name must match the runtime connection")
}

func TestSpotifyConnectionAndCredentialsCannotSerializeOrLeak(t *testing.T) {
	connection := factoryConnection(t)
	encoded, err := json.Marshal(connection)
	require.ErrorContains(t, err, "cannot be serialized")
	require.Nil(t, encoded)
	credentials := spotify.Credentials{AccessToken: sdkgo.NewSecretString("secret-token")}
	encoded, err = json.Marshal(credentials)
	require.Error(t, err)
	require.Nil(t, encoded)
	require.NotContains(t, fmt.Sprintf("%#v", credentials), "secret-token")
	require.Equal(t, "spotify.Connection{[REDACTED]}", fmt.Sprintf("%#v", connection))
}

func factoryConnection(t *testing.T) spotify.Connection {
	t.Helper()
	client, err := spotify.New(spotify.Config{}, testCredentials())
	require.NoError(t, err)
	connection, err := spotify.NewConnection(client, spotifyConnection)
	require.NoError(t, err)
	return connection
}
