// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ConversationCreatedTriggerConfiguration is the conversationCreated binding configuration that the
// channel slug entry saves in Dex Web. Changing the channel of a binding that already polled makes its
// next poll record a new baseline for the new channel.
type ConversationCreatedTriggerConfiguration struct {
	// Channel is the slug of the watched Re:amaze channel, Re:amaze's category, such as support; blank
	// watches every channel of the connection's brand.
	Channel string `json:"channel,omitempty"`
}

// Validate rejects a channel that is not a Re:amaze channel slug; blank is valid.
func (configuration ConversationCreatedTriggerConfiguration) Validate() error {
	if configuration.Channel == "" {
		return nil
	}
	return validateChannelSlug("channel", configuration.Channel)
}

// ConversationCreatedEvent is one conversation created in the watched brand or channel. Conversation is
// the list entry Re:amaze returned when the poll first saw it, including archived and spam conversations,
// so an application filters on Conversation.Status when it needs to.
type ConversationCreatedEvent struct {
	// Brand is the connection's Re:amaze brand subdomain, such as acme for https://acme.reamaze.io.
	Brand string `json:"brand"`
	// Channel is the watched channel slug, or empty when the binding watches every channel.
	Channel string `json:"channel,omitempty"`
	// Conversation is the new conversation with its first message, cut at MaxTextBytes.
	Conversation Conversation `json:"conversation"`
}

// newConversationCreatedTriggerEvent converts one new conversation. Its ID is a pure function of the
// brand and slug, so a refetch after a crash yields the same ID.
func newConversationCreatedTriggerEvent(brand string, channel string, conversation Conversation) sdkgo.TriggerEvent[ConversationCreatedEvent] {
	return sdkgo.TriggerEvent[ConversationCreatedEvent]{
		ID:         conversationCreatedEventID(brand, conversation.ID),
		OccurredAt: conversation.CreatedAt,
		Payload:    ConversationCreatedEvent{Brand: brand, Channel: channel, Conversation: conversation},
	}
}

// conversationCreatedEventID hashes the brand and slug, so it stays short for any slug length.
func conversationCreatedEventID(brand string, slug string) string {
	digest := sha256.Sum256([]byte(brand + "\x00" + slug))
	return "reamaze-conversation-" + hex.EncodeToString(digest[:16])
}

func conversationCreatedTriggerConfigurationError(err error) error {
	return fmt.Errorf("reamaze conversationCreated configuration: %w", err)
}
