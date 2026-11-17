// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package discord_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/discord/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = "/api/v10"
const conformanceFixturePathPrefix = "/api/v10"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := discord.New(discord.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[discord.Credentials]{
		discordConnection: {BotToken: sdkgo.NewSecretString(testBotToken)},
	}, discord.WithHTTPClient(transport))
	require.NoError(t, err)
	context := newStepDexContext("conformance")
	createContext := singledispatchtest.NewContext("discord-conformance", "create-guild-channel")
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if scenario.Operation == "createGuildChannel" && attempt > 1 {
			createContext.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"listGuildChannels":     newConformanceQueryInvocation(t, scenario.Input, context, client.ListGuildChannels(), discordConnection),
		"createGuildChannel":    newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateGuildChannel(), discordConnection, scenario.CallID),
		"updateChannel":         newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateChannel(), discordConnection, scenario.CallID),
		"deleteChannel":         newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteChannel(), discordConnection, scenario.CallID),
		"listGuildMembers":      newConformanceQueryInvocation(t, scenario.Input, context, client.ListGuildMembers(), discordConnection),
		"banGuildMember":        newConformanceMutationInvocation(t, scenario.Input, context, client.BanGuildMember(), discordConnection, scenario.CallID),
		"kickGuildMember":       newConformanceMutationInvocation(t, scenario.Input, context, client.KickGuildMember(), discordConnection, scenario.CallID),
		"addGuildMemberRole":    newConformanceMutationInvocation(t, scenario.Input, context, client.AddGuildMemberRole(), discordConnection, scenario.CallID),
		"removeGuildMemberRole": newConformanceMutationInvocation(t, scenario.Input, context, client.RemoveGuildMemberRole(), discordConnection, scenario.CallID),
		"timeoutGuildMember":    newConformanceMutationInvocation(t, scenario.Input, context, client.TimeoutGuildMember(), discordConnection, scenario.CallID),
		"unbanGuildMember":      newConformanceMutationInvocation(t, scenario.Input, context, client.UnbanGuildMember(), discordConnection, scenario.CallID),
		"getMessage":            newConformanceQueryInvocation(t, scenario.Input, context, client.GetMessage(), discordConnection),
		"deleteMessage":         newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteMessage(), discordConnection, scenario.CallID),
		"getChannel":            newConformanceQueryInvocation(t, scenario.Input, context, client.GetChannel(), discordConnection),
		"listChannelMessages":   newConformanceQueryInvocation(t, scenario.Input, context, client.ListChannelMessages(), discordConnection),
		"createMessage":         newConformanceMutationInvocation(t, scenario.Input, context, client.CreateMessage(), discordConnection, scenario.CallID),
		"addReaction":           newConformanceMutationInvocation(t, scenario.Input, context, client.AddReaction(), discordConnection, scenario.CallID),
	}}
}
