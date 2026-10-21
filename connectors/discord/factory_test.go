// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package discord_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/discord"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type channelTarget struct {
	dex.StepDefaultsNoWaitFor[discord.GetChannelResult]
}

func (channelTarget) Execute(dex.Context, discord.GetChannelResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type messagesTarget struct {
	dex.StepDefaultsNoWaitFor[discord.ListChannelMessagesResult]
}

func (messagesTarget) Execute(dex.Context, discord.ListChannelMessagesResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type messageTarget struct {
	dex.StepDefaultsNoWaitFor[discord.CreateMessageResult]
}

func (messageTarget) Execute(dex.Context, discord.CreateMessageResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type reactionTarget struct {
	dex.StepDefaultsNoWaitFor[discord.AddReactionResult]
}

func (reactionTarget) Execute(dex.Context, discord.AddReactionResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestFactoriesRequireOnlyTheHappyPathBranch(t *testing.T) {
	connection := newTestConnection(t)
	annotations := sdkgo.StepAnnotations{GroupID: "discord", GroupLabel: "Discord", Explanation: "Call Discord."}
	require.NotPanics(t, func() {
		discord.NewGetChannelStep(discord.GetChannelStepConfig[string]{
			StepType: "Get", Annotations: annotations, Connection: connection, ConnectionName: discordConnection.Name,
			MapToOperationInput: func(string) discord.GetChannelInput { return discord.GetChannelInput{} },
			Found:               sdkgo.GoTo(channelTarget{}),
		})
		discord.NewListChannelMessagesStep(discord.ListChannelMessagesStepConfig[string]{
			StepType: "List", Annotations: annotations, Connection: connection, ConnectionName: discordConnection.Name,
			MapToOperationInput: func(string) discord.ListChannelMessagesInput { return discord.ListChannelMessagesInput{} },
			Listed:              sdkgo.GoTo(messagesTarget{}),
		})
		discord.NewCreateMessageStep(discord.CreateMessageStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: discordConnection.Name,
			MapToOperationInput: func(string) discord.CreateMessageInput { return discord.CreateMessageInput{} },
			Created:             sdkgo.GoTo(messageTarget{}),
		})
		discord.NewAddReactionStep(discord.AddReactionStepConfig[string]{
			StepType: "React", Annotations: annotations, Connection: connection, ConnectionName: discordConnection.Name,
			MapToOperationInput: func(string) discord.AddReactionInput { return discord.AddReactionInput{} },
			Added:               sdkgo.GoTo(reactionTarget{}),
		})
	})
	require.Panics(t, func() {
		discord.NewCreateMessageStep(discord.CreateMessageStepConfig[string]{
			StepType: "Create", Annotations: annotations, Connection: connection, ConnectionName: discordConnection.Name,
			MapToOperationInput: func(string) discord.CreateMessageInput { return discord.CreateMessageInput{} },
			ProviderRejected:    sdkgo.GoTo(messageTarget{}),
		})
	}, "the created branch is required")
}

func TestOperationDefinitionsMatchTheManifest(t *testing.T) {
	require.Equal(t, dex.StepDurabilityAsync, discord.CreateMessageDefinition.StepDefaults.ExecuteDurability)
	require.LessOrEqual(t, discord.CreateMessageDefinition.StepDefaults.ExecuteRetry.TotalDuration.Minutes(), 2.0,
		"every createMessage retry must fit Discord's few-minute nonce window")
}

func newTestConnection(t *testing.T) discord.Connection {
	t.Helper()
	client := newTestClient(t, "http://127.0.0.1:1", discord.Config{})
	connection, err := discord.NewConnection(client, discordConnection)
	require.NoError(t, err)
	return connection
}
