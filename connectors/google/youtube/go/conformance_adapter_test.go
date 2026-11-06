// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package youtube_test

import (
    "net/http"
    "testing"

    youtube "github.com/superdurable/dex-connectors-library/connectors/google/youtube/go"
    "github.com/superdurable/dex-connectors-library/sdkgo"
    "github.com/stretchr/testify/require"
)

const conformanceEndpointPath = "/youtube/v3"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
    client, err := youtube.New(youtube.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[youtube.Credentials]{
        youtubeConnection: {AuthMethodID: youtube.ManageAuthMethodID, AccessToken: sdkgo.NewSecretString("youtube-token")},
    }, youtube.WithHTTPClient(transport))
    require.NoError(t, err)
    return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
        "searchVideos": newConformanceQueryInvocation(t, scenario.Input, newDexContext("conformance"), client.SearchVideos(), youtubeConnection),
        "getVideo": newConformanceQueryInvocation(t, scenario.Input, newDexContext("conformance"), client.GetVideo(), youtubeConnection),
        "getChannel": newConformanceQueryInvocation(t, scenario.Input, newDexContext("conformance"), client.GetChannel(), youtubeConnection),
        "listPlaylistItems": newConformanceQueryInvocation(t, scenario.Input, newDexContext("conformance"), client.ListPlaylistItems(), youtubeConnection),
        "addPlaylistItem": newConformanceMutationInvocation(t, scenario.Input, newDexContext("conformance"), client.AddPlaylistItem(), youtubeConnection, scenario.CallID),
    }}
}
