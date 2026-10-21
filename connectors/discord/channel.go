// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package discord

import (
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetChannelInput identifies one channel.
type GetChannelInput struct {
	// ChannelID is the channel's snowflake ID, 17 to 20 digits, from Copy Channel ID in Discord's Developer Mode.
	ChannelID string `json:"channelId"`
}

// Channel is the part of a Discord channel object that Flows route on.
// Name and Topic are untrusted provider text: escape them before rendering HTML.
type Channel struct {
	// ID is the channel's snowflake ID.
	ID string `json:"id"`
	// Type is Discord's channel type number, such as 0 for a server text channel or 5 for an announcement channel.
	Type int `json:"type"`
	// GuildID is the server's snowflake ID, blank for a direct message channel.
	GuildID string `json:"guildId,omitempty"`
	// Name is the channel name without the leading #, blank for a direct message channel.
	Name string `json:"name,omitempty"`
	// Topic is the channel topic, blank when none is set.
	Topic string `json:"topic,omitempty"`
	// LastMessageID is the snowflake ID of the newest message, blank when unknown or empty.
	LastMessageID string `json:"lastMessageId,omitempty"`
}

// providerChannel is one channel in Discord's response JSON.
type providerChannel struct {
	ID            string  `json:"id"`
	Type          *int    `json:"type"`
	GuildID       *string `json:"guild_id"`
	Name          *string `json:"name"`
	Topic         *string `json:"topic"`
	LastMessageID *string `json:"last_message_id"`
}

// GetChannelOperation implements the getChannel connector Query.
type GetChannelOperation struct{ client *Client }

var getChannelBranches = operationBranches{
	notFound: GetChannelBranchNotFound, rejected: GetChannelBranchProviderRejected,
	invalidResponse: GetChannelBranchInvalidResponse, defect: GetChannelBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (GetChannelOperation) Definition() sdkgo.QueryDefinition { return GetChannelDefinition }

// Invoke sends one channel read and classifies its attempt. An invalid
// channel ID or credential selects defect before any request.
func (operation GetChannelOperation) Invoke(call sdkgo.Call, input GetChannelInput) sdkgo.QueryAttempt[Channel] {
	if err := validateSnowflake("channelId", input.ChannelID); err != nil {
		return sdkgo.NewQueryBranch(GetChannelBranchDefect, Channel{}, providerFailurePointer("getChannel", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "getChannel", providerRequest{
		method: http.MethodGet, path: "/channels/" + input.ChannelID,
	}, input.ChannelID)
	if outcome != nil {
		return queryAttemptForOutcome[Channel](*outcome, getChannelBranches)
	}
	receipt := operation.client.receipt(call, input.ChannelID)
	channel, err := decodeChannel(response.body, input.ChannelID)
	if err != nil {
		return sdkgo.NewQueryBranch(GetChannelBranchInvalidResponse, Channel{}, providerFailurePointer("getChannel", sdkgo.FailureProtocol, err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(GetChannelBranchFound, channel, nil, receipt)
}

func decodeChannel(body []byte, channelID string) (Channel, error) {
	var decoded providerChannel
	if err := decodeResponse(body, &decoded); err != nil {
		return Channel{}, err
	}
	if decoded.ID != channelID {
		return Channel{}, errors.New("Discord returned a different channel than requested")
	}
	if decoded.Type == nil {
		return Channel{}, errors.New("Discord returned a channel without a type")
	}
	channel := Channel{
		ID: decoded.ID, Type: *decoded.Type, GuildID: optionalText(decoded.GuildID), Name: optionalText(decoded.Name),
		Topic: optionalText(decoded.Topic), LastMessageID: optionalText(decoded.LastMessageID),
	}
	for _, identifier := range []string{channel.GuildID, channel.LastMessageID} {
		if identifier != "" && !snowflakePattern.MatchString(identifier) {
			return Channel{}, errors.New("Discord returned a channel with an invalid snowflake ID")
		}
	}
	return channel, nil
}

func optionalText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
