// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spotify

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/spotify/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v1"
const conformanceFixturePathPrefix = ""

var conformanceConnection = sdkgo.ConnectionRef{Provider: "spotify", Name: "playlist-reader"}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := New(Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		conformanceConnection: {AccessToken: sdkgo.NewSecretString("one-use-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listUserPlaylists": newConformanceQueryInvocation(t, scenario.Input, testsupport.NewDexContext("spotify-conformance", "list-user-playlists"), client.ListUserPlaylists(), conformanceConnection),
		"searchPlaylists": newConformanceQueryInvocation(t, scenario.Input, testsupport.NewDexContext("spotify-conformance", "search-playlists"), client.SearchPlaylists(), conformanceConnection),
		"getPlaylist": newConformanceQueryInvocation(t, scenario.Input, testsupport.NewDexContext("spotify-conformance", "get-playlist"), client.GetPlaylist(), conformanceConnection),
		"listPlaylistTracks": newConformanceQueryInvocation(t, scenario.Input, testsupport.NewDexContext("spotify-conformance", "list-playlist-tracks"), client.ListPlaylistTracks(), conformanceConnection),
	}}
}
