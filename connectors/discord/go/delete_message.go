// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package discord

import (
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// DeleteMessageInput identifies the message to delete.
type DeleteMessageInput struct {
	// ChannelID is the channel snowflake copied from Discord Developer Mode.
	ChannelID string `json:"channelId"`
	// MessageID is the message snowflake copied from Discord Developer Mode.
	MessageID string `json:"messageId"`
}

// DeletedMessage identifies the message Discord deleted.
type DeletedMessage struct {
	// ChannelID is the message's channel snowflake.
	ChannelID string `json:"channelId"`
	// MessageID is the deleted message snowflake.
	MessageID string `json:"messageId"`
}

// DeleteMessageOperation implements the deleteMessage connector Mutation.
type DeleteMessageOperation struct{ client *Client }

var deleteMessageBranches = operationBranches{
	notFound: DeleteMessageBranchNotFound, rejected: DeleteMessageBranchProviderRejected,
	defect: DeleteMessageBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (DeleteMessageOperation) Definition() sdkgo.MutationDefinition { return DeleteMessageDefinition }

// IdempotencyKey derives a stable key; Discord's DELETE converges without sending it.
func (DeleteMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes one message; repeated attempts cannot delete another object.
func (operation DeleteMessageOperation) Invoke(call sdkgo.Call, input DeleteMessageInput) sdkgo.MutationAttempt[DeletedMessage] {
	if err := validateMessageIdentity(input.ChannelID, input.MessageID); err != nil {
		return sdkgo.NewMutationBranch(DeleteMessageBranchDefect, DeletedMessage{}, providerFailurePointer("deleteMessage", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	_, outcome := operation.client.exchange(call, "deleteMessage", providerRequest{
		method: http.MethodDelete, path: messagePath(input.ChannelID, input.MessageID),
	}, input.MessageID)
	if outcome != nil {
		return mutationAttemptForOutcome[DeletedMessage](*outcome, deleteMessageBranches)
	}
	return sdkgo.NewMutationBranch(DeleteMessageBranchDeleted, DeletedMessage{ChannelID: input.ChannelID, MessageID: input.MessageID}, nil, operation.client.receipt(call, input.MessageID))
}
