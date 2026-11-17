// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package discord

import (
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetMessageInput identifies one message in a Discord channel.
type GetMessageInput struct {
	// ChannelID is the channel snowflake copied from Discord Developer Mode.
	ChannelID string `json:"channelId"`
	// MessageID is the message snowflake copied from Discord Developer Mode.
	MessageID string `json:"messageId"`
}

// GetMessageOperation implements the getMessage connector Query.
type GetMessageOperation struct{ client *Client }

var getMessageBranches = operationBranches{
	notFound: GetMessageBranchNotFound, rejected: GetMessageBranchProviderRejected,
	invalidResponse: GetMessageBranchInvalidResponse, defect: GetMessageBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (GetMessageOperation) Definition() sdkgo.QueryDefinition { return GetMessageDefinition }

// Invoke reads one message and validates its identity and shape.
func (operation GetMessageOperation) Invoke(call sdkgo.Call, input GetMessageInput) sdkgo.QueryAttempt[Message] {
	if err := validateMessageIdentity(input.ChannelID, input.MessageID); err != nil {
		return sdkgo.NewQueryBranch(GetMessageBranchDefect, Message{}, providerFailurePointer("getMessage", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "getMessage", providerRequest{
		method: http.MethodGet, path: messagePath(input.ChannelID, input.MessageID),
	}, input.MessageID)
	if outcome != nil {
		return queryAttemptForOutcome[Message](*outcome, getMessageBranches)
	}
	var providerMessage providerMessage
	if err := decodeResponse(response.body, &providerMessage); err != nil {
		return sdkgo.NewQueryBranch(GetMessageBranchInvalidResponse, Message{}, providerFailurePointer("getMessage", sdkgo.FailureProtocol, err.Error()), operation.client.receipt(call, input.MessageID))
	}
	message, err := convertProviderMessage(providerMessage, input.ChannelID)
	if err == nil && message.ID != input.MessageID {
		err = errors.New("Discord returned a different message than requested")
	}
	if err != nil {
		return sdkgo.NewQueryBranch(GetMessageBranchInvalidResponse, Message{}, providerFailurePointer("getMessage", sdkgo.FailureProtocol, err.Error()), operation.client.receipt(call, input.MessageID))
	}
	return sdkgo.NewQueryBranch(GetMessageBranchFound, message, nil, operation.client.receipt(call, input.MessageID))
}

func validateMessageIdentity(channelID, messageID string) error {
	if err := validateSnowflake("channelId", channelID); err != nil {
		return err
	}
	return validateSnowflake("messageId", messageID)
}

func messagePath(channelID, messageID string) string {
	return "/channels/" + channelID + "/messages/" + messageID
}
