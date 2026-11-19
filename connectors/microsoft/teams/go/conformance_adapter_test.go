// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teams_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/teams/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v1.0"
const conformanceFixturePathPrefix = "/v1.0"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := teams.New(teams.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[teams.Credentials]{
		teamsConnection: {AccessToken: sdkgo.NewSecretString(testAccessToken)},
	}, teams.WithHTTPClient(transport))
	require.NoError(t, err)
	dexContext := newTestDexContext("conformance")
	if scenario.FixedClock != "" {
		dexContext.firstAttemptAt, err = time.Parse(time.RFC3339Nano, scenario.FixedClock)
		require.NoError(t, err)
	}
	prior := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(raw []byte) { dexContext.heartbeat = raw })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { dexContext.attempt = prior + int32(attempt) },
		actions: map[string]conformanceOperationInvocation{
			"listChannels":        newConformanceQueryInvocation(t, scenario.Input, dexContext, client.ListChannels(), teamsConnection),
			"getChannel":          newConformanceQueryInvocation(t, scenario.Input, dexContext, client.GetChannel(), teamsConnection),
			"listChannelMessages": newConformanceQueryInvocation(t, scenario.Input, dexContext, client.ListChannelMessages(), teamsConnection),
			"getChannelMessage":   newConformanceQueryInvocation(t, scenario.Input, dexContext, client.GetChannelMessage(), teamsConnection),
			"listChats":           newConformanceQueryInvocation(t, scenario.Input, dexContext, client.ListChats(), teamsConnection),
			"getChat":             newConformanceQueryInvocation(t, scenario.Input, dexContext, client.GetChat(), teamsConnection),
			"listThreadReplies":   newConformanceQueryInvocation(t, scenario.Input, dexContext, client.ListThreadReplies(), teamsConnection),
			"postChannelMessage":  newConformanceMutationInvocation(t, scenario.Input, dexContext, client.PostChannelMessage(), teamsConnection, scenario.CallID),
			"postThreadReply":     newConformanceMutationInvocation(t, scenario.Input, dexContext, client.PostThreadReply(), teamsConnection, scenario.CallID),
			"postChatMessage":     newConformanceMutationInvocation(t, scenario.Input, dexContext, client.PostChatMessage(), teamsConnection, scenario.CallID),
		},
	}
}
