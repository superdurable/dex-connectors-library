// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package channelmarkersearch

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/slack/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestFlowUsesStaticConnectionAndStepIdentities(t *testing.T) {
	reference := sdkgo.ConnectionRef{Provider: "slack", Name: ConnectionName}
	client, err := slack.New(slack.Config{}, sdkgo.StaticCredentialProvider[slack.Credentials]{})
	require.NoError(t, err)
	connection, err := slack.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	_, err = dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	require.Equal(t, "SlackChannelMarkerSearch", dex.GetFinalFlowType(flow))
	require.Equal(t, "StartSlackChannelMarkerSearch", dex.GetFinalStepType[SearchRequest](startSearch{}))
	require.Equal(t, "RecordSlackMarkerSearchBot", dex.GetFinalStepType[slack.GetWorkspaceIdentityResult](recordBotIdentity{}))
	require.Equal(t, "InspectSlackChannelHistoryPage", dex.GetFinalStepType[slack.ListChannelMessagesResult](inspectPage{}))
	wait, err := startSearch{}.WaitFor(nil, SearchRequest{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFindMarkerMessageReturnsTheBotsNewestMatch(t *testing.T) {
	bot := slack.GetWorkspaceIdentityOutput{BotUserID: "UBOT", BotID: "B123"}
	messages := []slack.ChannelMessage{
		{Timestamp: "5.0", UserID: "U2", Text: "Did ref:order-1042 post?"},
		{Timestamp: "4.0", BotID: "B999", Text: "Another app quoted ref:order-1042"},
		{Timestamp: "3.0", UserID: "UBOT", BotID: "B123", Text: "unrelated"},
		{Timestamp: "2.0", UserID: "UBOT", BotID: "B123", Text: "Order ready ref:order-1042"},
		{Timestamp: "1.0", UserID: "UBOT", Text: "older ref:order-1042"},
	}
	message, isFound := FindMarkerMessage(messages, "ref:order-1042", bot)
	require.True(t, isFound)
	require.Equal(t, "2.0", message.Timestamp)
	_, isFound = FindMarkerMessage(messages, "REF:ORDER-1042", bot)
	require.False(t, isFound)
	_, isFound = FindMarkerMessage(messages[:2], "ref:order-1042", bot)
	require.False(t, isFound, "a person or another app quoting the marker is not the app's post")
}

func TestIsPostedByBotMatchesTheBotIDOrBotUserID(t *testing.T) {
	bot := slack.GetWorkspaceIdentityOutput{BotUserID: "UBOT", BotID: "B123"}
	require.True(t, IsPostedByBot(slack.ChannelMessage{UserID: "UBOT"}, bot))
	require.True(t, IsPostedByBot(slack.ChannelMessage{BotID: "B123"}, bot))
	require.False(t, IsPostedByBot(slack.ChannelMessage{UserID: "U2"}, bot))
	require.False(t, IsPostedByBot(slack.ChannelMessage{}, slack.GetWorkspaceIdentityOutput{}))
}

func TestHistoryPageInputCarriesTheTimeBoundAndCursor(t *testing.T) {
	request := SearchRequest{ChannelID: "C123", Marker: "ref", OldestTimestamp: "1710000000.000100", MaximumPages: 2}
	require.Equal(t, slack.ListChannelMessagesInput{ChannelID: "C123", OldestTimestamp: "1710000000.000100", Cursor: "next"}, historyPageInput(request, "next"))
	require.Equal(t, historyPageInput(request, ""), MapToListChannelMessagesInput(historyPageInput(request, "")))
	require.Equal(t, slack.GetWorkspaceIdentityInput{}, MapToWorkspaceIdentityInput(request))
}
