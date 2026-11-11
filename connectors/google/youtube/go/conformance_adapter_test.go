// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package youtube_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	youtube "github.com/superdurable/dex-connectors-library/connectors/google/youtube/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = "/youtube/v3"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := youtube.New(youtube.Config{Endpoint: endpoint, MaxResponseBytes: conformanceMaxResponseBytes(scenario)}, sdkgo.StaticCredentialProvider[youtube.Credentials]{
		youtubeConnection: {AuthMethodID: youtube.ManageAuthMethodID, AccessToken: sdkgo.NewSecretString("youtube-token")},
	}, youtube.WithHTTPClient(transport))
	require.NoError(t, err)
	context := singledispatchtest.NewContext("youtube-flow", "conformance")
	prior := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(raw []byte) {
		require.NoError(t, context.SetPersistedCheckpoint(json.RawMessage(raw)))
	})
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		for context.Attempt() < prior+int32(attempt) {
			context.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"searchVideos":      newConformanceQueryInvocation(t, scenario.Input, context, client.SearchVideos(), youtubeConnection),
		"getVideo":          newConformanceQueryInvocation(t, scenario.Input, context, client.GetVideo(), youtubeConnection),
		"getChannel":        newConformanceQueryInvocation(t, scenario.Input, context, client.GetChannel(), youtubeConnection),
		"listPlaylistItems": newConformanceQueryInvocation(t, scenario.Input, context, client.ListPlaylistItems(), youtubeConnection),
		"addPlaylistItem":   newConformanceMutationInvocation(t, scenario.Input, context, client.AddPlaylistItem(), youtubeConnection, scenario.CallID),
	}}
}

// conformanceMaxResponseBytes lowers the response limit for a scenario named "-oversized", so a short fixture exceeds it.
func conformanceMaxResponseBytes(scenario conformanceScenario) int64 {
	if strings.HasSuffix(scenario.Name, "-oversized") {
		return 120
	}
	return 0
}
