// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package discord

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListGuildChannelsInput identifies the server whose visible channels are listed.
type ListGuildChannelsInput struct {
	// GuildID is the server snowflake from Copy Server ID in Discord Developer Mode.
	GuildID string `json:"guildId"`
}

// GuildChannels contains one server's channels visible to the bot, excluding threads.
type GuildChannels struct {
	// Channels contains channel summaries in provider order.
	Channels []Channel `json:"channels"`
}

// ListGuildChannelsOperation lists channels in one server.
type ListGuildChannelsOperation struct{ client *Client }

var listGuildChannelsBranches = operationBranches{
	notFound: ListGuildChannelsBranchNotFound, rejected: ListGuildChannelsBranchProviderRejected,
	invalidResponse: ListGuildChannelsBranchInvalidResponse, defect: ListGuildChannelsBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (ListGuildChannelsOperation) Definition() sdkgo.QueryDefinition {
	return ListGuildChannelsDefinition
}

// Invoke lists all channels the bot can view in a server.
func (operation ListGuildChannelsOperation) Invoke(call sdkgo.Call, input ListGuildChannelsInput) sdkgo.QueryAttempt[GuildChannels] {
	if err := validateSnowflake("guildId", input.GuildID); err != nil {
		return sdkgo.NewQueryBranch(ListGuildChannelsBranchDefect, GuildChannels{}, providerFailurePointer("listGuildChannels", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "listGuildChannels", providerRequest{method: http.MethodGet, path: "/guilds/" + input.GuildID + "/channels"}, input.GuildID)
	if outcome != nil {
		return queryAttemptForOutcome[GuildChannels](*outcome, listGuildChannelsBranches)
	}
	var decoded []providerChannel
	if err := decodeResponse(response.body, &decoded); err != nil || decoded == nil {
		return sdkgo.NewQueryBranch(ListGuildChannelsBranchInvalidResponse, GuildChannels{}, providerFailurePointer("listGuildChannels", sdkgo.FailureProtocol, "Discord returned an invalid channel list"), operation.client.receipt(call, input.GuildID))
	}
	channels := make([]Channel, 0, len(decoded))
	for _, item := range decoded {
		channel, err := decodeChannelItem(item)
		if err != nil || (channel.GuildID != "" && channel.GuildID != input.GuildID) {
			return sdkgo.NewQueryBranch(ListGuildChannelsBranchInvalidResponse, GuildChannels{}, providerFailurePointer("listGuildChannels", sdkgo.FailureProtocol, "Discord returned an invalid channel in the list"), operation.client.receipt(call, input.GuildID))
		}
		channel.GuildID = input.GuildID
		channels = append(channels, channel)
	}
	return sdkgo.NewQueryBranch(ListGuildChannelsBranchListed, GuildChannels{Channels: channels}, nil, operation.client.receipt(call, input.GuildID))
}

// UpdateChannelInput replaces a guild channel's name or topic; nil keeps that field.
type UpdateChannelInput struct {
	// ChannelID is the existing guild channel snowflake.
	ChannelID string `json:"channelId"`
	// Name is a replacement channel name of 1 to 100 characters.
	Name *string `json:"name,omitempty"`
	// Topic is a replacement text-channel topic of at most 1024 characters; empty clears it.
	Topic *string `json:"topic,omitempty"`
}

// UpdateChannelOperation replaces selected channel fields.
type UpdateChannelOperation struct{ client *Client }

var updateChannelBranches = operationBranches{
	notFound: UpdateChannelBranchNotFound, rejected: UpdateChannelBranchProviderRejected,
	invalidResponse: UpdateChannelBranchInvalidResponse, defect: UpdateChannelBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (UpdateChannelOperation) Definition() sdkgo.MutationDefinition { return UpdateChannelDefinition }

// IdempotencyKey identifies the call; absolute channel fields are safe to repeat.
func (UpdateChannelOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateChannelInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one absolute channel update.
func (operation UpdateChannelOperation) Invoke(call sdkgo.Call, input UpdateChannelInput) sdkgo.MutationAttempt[Channel] {
	if err := validateSnowflake("channelId", input.ChannelID); err != nil || (input.Name == nil && input.Topic == nil) || (input.Name != nil && (strings.TrimSpace(*input.Name) == "" || len([]rune(*input.Name)) > 100 || !utf8.ValidString(*input.Name))) || (input.Topic != nil && (len([]rune(*input.Topic)) > 1024 || !utf8.ValidString(*input.Topic))) {
		return sdkgo.NewMutationBranch(UpdateChannelBranchDefect, Channel{}, providerFailurePointer("updateChannel", sdkgo.FailureValidation, "valid channel ID and replacement name or topic are required"), sdkgo.Receipt{})
	}
	body := map[string]string{}
	if input.Name != nil {
		body["name"] = *input.Name
	}
	if input.Topic != nil {
		body["topic"] = *input.Topic
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateChannelBranchDefect, Channel{}, providerFailurePointer("updateChannel", sdkgo.FailureLocalDefect, "channel update could not be encoded"), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "updateChannel", providerRequest{method: http.MethodPatch, path: "/channels/" + input.ChannelID, body: encoded}, input.ChannelID)
	if outcome != nil {
		return mutationAttemptForOutcome[Channel](*outcome, updateChannelBranches)
	}
	channel, err := decodeChannel(response.body, input.ChannelID)
	if err != nil || (input.Name != nil && channel.Name != *input.Name) || (input.Topic != nil && channel.Topic != *input.Topic) {
		return sdkgo.NewMutationBranch(UpdateChannelBranchInvalidResponse, Channel{}, providerFailurePointer("updateChannel", sdkgo.FailureProtocol, "Discord returned a channel inconsistent with the update"), operation.client.receipt(call, input.ChannelID))
	}
	return sdkgo.NewMutationBranch(UpdateChannelBranchUpdated, channel, nil, operation.client.receipt(call, input.ChannelID))
}

// DeleteChannelInput identifies one guild channel to delete.
type DeleteChannelInput struct {
	// ChannelID is the guild channel snowflake from Copy Channel ID.
	ChannelID string `json:"channelId"`
}

// DeleteChannelOperation deletes one guild channel.
type DeleteChannelOperation struct{ client *Client }

var deleteChannelBranches = operationBranches{
	notFound: DeleteChannelBranchNotFound, rejected: DeleteChannelBranchProviderRejected,
	invalidResponse: DeleteChannelBranchInvalidResponse, defect: DeleteChannelBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (DeleteChannelOperation) Definition() sdkgo.MutationDefinition { return DeleteChannelDefinition }

// IdempotencyKey identifies the call; deleting the same channel cannot delete another.
func (DeleteChannelOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteChannelInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes one channel and returns the summary Discord returns.
func (operation DeleteChannelOperation) Invoke(call sdkgo.Call, input DeleteChannelInput) sdkgo.MutationAttempt[Channel] {
	if err := validateSnowflake("channelId", input.ChannelID); err != nil {
		return sdkgo.NewMutationBranch(DeleteChannelBranchDefect, Channel{}, providerFailurePointer("deleteChannel", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "deleteChannel", providerRequest{method: http.MethodDelete, path: "/channels/" + input.ChannelID}, input.ChannelID)
	if outcome != nil {
		return mutationAttemptForOutcome[Channel](*outcome, deleteChannelBranches)
	}
	channel, err := decodeChannel(response.body, input.ChannelID)
	if err != nil {
		return sdkgo.NewMutationBranch(DeleteChannelBranchInvalidResponse, Channel{}, providerFailurePointer("deleteChannel", sdkgo.FailureProtocol, "Discord returned an invalid deleted channel"), operation.client.receipt(call, input.ChannelID))
	}
	return sdkgo.NewMutationBranch(DeleteChannelBranchDeleted, channel, nil, operation.client.receipt(call, input.ChannelID))
}
