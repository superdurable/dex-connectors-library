// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spotify_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/spotify/go"
	"github.com/superdurable/dex-connectors-library/connectors/spotify/go/internal/testsupport"
)

const conformanceEndpointPath = "/v1"
const conformanceFixturePathPrefix = "/v1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := spotify.New(spotify.Config{Endpoint: endpoint}, testCredentials(), spotify.WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listPlaylistTracks": newConformanceQueryInvocation(t, scenario.Input, testsupport.NewDexContext("conformance", "playlist-step"), client.ListPlaylistTracks(), spotifyConnection),
	}}
}
