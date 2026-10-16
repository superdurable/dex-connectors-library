// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package channelannouncement

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/discord"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testChannelID = "112233445566778899"

func TestPrepareAnnouncementRequestTrimsAndAppliesTheDefaultReaction(t *testing.T) {
	prepared, err := PrepareAnnouncementRequest(AnnouncementRequest{ChannelID: " " + testChannelID + " ", Text: "Release 1.2 is out"})
	require.NoError(t, err)
	require.Equal(t, AnnouncementRequest{ChannelID: testChannelID, Text: "Release 1.2 is out", ReactionEmoji: "📣"}, prepared)

	prepared, err = PrepareAnnouncementRequest(AnnouncementRequest{ChannelID: testChannelID, Text: "x", ReactionEmoji: "👍"})
	require.NoError(t, err)
	require.Equal(t, "👍", prepared.ReactionEmoji)
}

func TestPrepareAnnouncementRequestRejectsInvalidInput(t *testing.T) {
	for name, request := range map[string]AnnouncementRequest{
		"blank channel": {Text: "hi"},
		"channel name":  {ChannelID: "#announcements", Text: "hi"},
		"short channel": {ChannelID: "1234", Text: "hi"},
		"blank text":    {ChannelID: testChannelID, Text: "  "},
		"text too long": {ChannelID: testChannelID, Text: strings.Repeat("a", discord.MaximumMessageCharacters+1)},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := PrepareAnnouncementRequest(request)
			require.Error(t, err)
		})
	}
}

func TestMappersAndConfirmation(t *testing.T) {
	request := AnnouncementRequest{ChannelID: testChannelID, Text: "Release", ReactionEmoji: "📣"}
	require.Equal(t, discord.GetChannelInput{ChannelID: testChannelID}, MapToGetChannelInput(request))
	require.Equal(t, discord.CreateMessageInput{ChannelID: testChannelID, Content: "Release"}, MapToCreateMessageInput(request))
	page := discord.MessagePage{Messages: []discord.Message{{ID: "998877665544332211"}, {ID: "998877665544332200"}}}
	require.True(t, ContainsMessage(page, "998877665544332200"))
	require.False(t, ContainsMessage(page, "998877665544332299"))
}

// TestStartFlowIdentitiesMatchTheFlowDefinition keeps registered types equal to the dexcli visualize names that Start Flow sends.
func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, "DiscordChannelAnnouncement", dex.GetFinalFlowType(NewFlow(discord.Connection{})))
	require.Equal(t, "RecordAnnouncementRequest", dex.GetFinalStepType[AnnouncementRequest](recordAnnouncementRequest{}))
	require.Equal(t, "AnnouncementChannelFound", dex.GetFinalStepType[discord.GetChannelResult](announcementChannelFound{}))
	require.Equal(t, "AnnouncementPosted", dex.GetFinalStepType[discord.CreateMessageResult](announcementPosted{}))
	require.Equal(t, "AnnouncementReacted", dex.GetFinalStepType[discord.AddReactionResult](announcementReacted{}))
	require.Equal(t, "CompleteAnnouncement", dex.GetFinalStepType[discord.ListChannelMessagesResult](completeAnnouncement{}))
	wait, err := recordAnnouncementRequest{}.WaitFor(nil, AnnouncementRequest{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithTheStaticConnectionName(t *testing.T) {
	reference := sdkgo.ConnectionRef{Provider: "discord", Name: ConnectionName}
	client, err := discord.New(discord.Config{}, sdkgo.StaticCredentialProvider[discord.Credentials]{
		reference: {BotToken: sdkgo.NewSecretString("MTAxTESTbot.token-part.0123456789abcdef")},
	})
	require.NoError(t, err)
	connection, err := discord.NewConnection(client, reference)
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)

	otherConnection, err := discord.NewConnection(client, sdkgo.ConnectionRef{Provider: "discord", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection)}) },
		"the static ConnectionName must match the runtime connection")
}
