// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package discord_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/discord/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/api/v10"
const conformanceFixturePathPrefix = "/api/v10"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := discord.New(discord.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[discord.Credentials]{
		discordConnection: {BotToken: sdkgo.NewSecretString(testBotToken)},
	}, discord.WithHTTPClient(transport))
	require.NoError(t, err)
	context := newStepDexContext("conformance")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getChannel":          newConformanceQueryInvocation(t, scenario.Input, context, client.GetChannel(), discordConnection),
		"listChannelMessages": newConformanceQueryInvocation(t, scenario.Input, context, client.ListChannelMessages(), discordConnection),
		"createMessage":       newConformanceMutationInvocation(t, scenario.Input, context, client.CreateMessage(), discordConnection, scenario.CallID),
		"addReaction":         newConformanceMutationInvocation(t, scenario.Input, context, client.AddReaction(), discordConnection, scenario.CallID),
	}}
}
