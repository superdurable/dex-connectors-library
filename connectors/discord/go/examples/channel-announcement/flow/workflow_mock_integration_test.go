//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package channelannouncement

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/discord/go"
	"github.com/superdurable/dex-connectors-library/connectors/discord/go/discordmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestChannelAnnouncementRunsOnMockDefaultsWithRealDex(t *testing.T) {
	mock := discordmock.New(t, ConnectionName)
	flow, harness := newAnnouncementIntegrationHarnessWithConnection(t, mock.Connection())
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	request := AnnouncementRequest{ChannelID: "100000000000000001", Text: "Release today @everyone"}
	result := harness.runFlow(t, ctx, flow, "discord-mock-"+strconv.FormatInt(time.Now().UnixNano(), 10), request)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome AnnouncementOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, request.ChannelID, outcome.ChannelID)
	require.Equal(t, "announcements", outcome.ChannelName)
	require.Equal(t, "300000000000000001", outcome.MessageID)
	require.Equal(t, "📣", outcome.ReactionEmoji)
	require.True(t, outcome.IsConfirmed, "the Flow must find its posted message in the read-back page")

	channelCalls := mock.GetChannel().Calls()
	require.Len(t, channelCalls, 1)
	require.Equal(t, discord.GetChannelInput{ChannelID: "100000000000000001"}, channelCalls[0].Input)
	postCalls := mock.CreateMessage().Calls()
	require.Len(t, postCalls, 1)
	require.Equal(t, discord.CreateMessageInput{ChannelID: "100000000000000001", Content: "Release today @everyone"}, postCalls[0].Input)
	reactionCalls := mock.AddReaction().Calls()
	require.Len(t, reactionCalls, 1)
	require.Equal(t, discord.AddReactionInput{ChannelID: "100000000000000001", MessageID: "300000000000000001", Emoji: "📣"}, reactionCalls[0].Input)
	readBackCalls := mock.ListChannelMessages().Calls()
	require.Len(t, readBackCalls, 1)
	require.Equal(t, discord.ListChannelMessagesInput{ChannelID: "100000000000000001", Limit: 10}, readBackCalls[0].Input)
}
