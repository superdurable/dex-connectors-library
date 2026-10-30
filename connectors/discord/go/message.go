// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package discord

import (
	"errors"
	"time"
)

// Message is the part of a Discord message object that Flows route on.
// Content is untrusted provider text: escape it before rendering HTML. A bot
// without the Message Content intent reads empty content for most server
// messages.
type Message struct {
	// ID is the message's snowflake ID.
	ID string `json:"id"`
	// ChannelID is the snowflake ID of the channel that holds the message.
	ChannelID string `json:"channelId"`
	// Author is the user or bot that sent the message.
	Author MessageAuthor `json:"author"`
	// Content is the message text, up to 2000 characters.
	Content string `json:"content"`
	// Timestamp is when Discord created the message, in UTC.
	Timestamp time.Time `json:"timestamp"`
	// EditedTimestamp is when the message was last edited, in UTC, or nil when it never was.
	EditedTimestamp *time.Time `json:"editedTimestamp,omitempty"`
	// Type is Discord's message type number, such as 0 for a default message or 19 for a reply.
	Type int `json:"type"`
}

// MessageAuthor identifies the sender of a message.
type MessageAuthor struct {
	// ID is the user's snowflake ID.
	ID string `json:"id"`
	// Username is the user's unique Discord username.
	Username string `json:"username"`
	// IsBot reports that the author is a bot account.
	IsBot bool `json:"isBot"`
}

// providerMessage is one message in Discord's response JSON.
type providerMessage struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	Author    *struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Bot      bool   `json:"bot"`
	} `json:"author"`
	Content         *string `json:"content"`
	Timestamp       string  `json:"timestamp"`
	EditedTimestamp *string `json:"edited_timestamp"`
	Type            *int    `json:"type"`
}

// convertProviderMessage validates one provider message and converts it to a Message.
func convertProviderMessage(message providerMessage, channelID string) (Message, error) {
	if !snowflakePattern.MatchString(message.ID) {
		return Message{}, errors.New("Discord returned a message without a valid snowflake ID")
	}
	if message.ChannelID != channelID {
		return Message{}, errors.New("Discord returned a message from a different channel")
	}
	if message.Author == nil || !snowflakePattern.MatchString(message.Author.ID) {
		return Message{}, errors.New("Discord returned a message without a valid author")
	}
	if message.Content == nil || message.Type == nil {
		return Message{}, errors.New("Discord returned a message without content or type")
	}
	timestamp, err := time.Parse(time.RFC3339Nano, message.Timestamp)
	if err != nil {
		return Message{}, errors.New("Discord returned a message without a valid timestamp")
	}
	converted := Message{
		ID: message.ID, ChannelID: message.ChannelID, Content: *message.Content, Timestamp: timestamp.UTC(), Type: *message.Type,
		Author: MessageAuthor{ID: message.Author.ID, Username: message.Author.Username, IsBot: message.Author.Bot},
	}
	if message.EditedTimestamp != nil {
		edited, err := time.Parse(time.RFC3339Nano, *message.EditedTimestamp)
		if err != nil {
			return Message{}, errors.New("Discord returned a message with an invalid edited timestamp")
		}
		edited = edited.UTC()
		converted.EditedTimestamp = &edited
	}
	return converted, nil
}
