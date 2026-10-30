// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver

import (
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const getConversationOperation = "getConversation"

// GetConversationInput identifies one conversation of a shared inbox.
type GetConversationInput struct {
	// InboxID is the Hiver shared inbox ID, as listInboxes returns it.
	InboxID string `json:"inboxId"`
	// ConversationID is the Hiver conversation ID or the shared mailbox user's Gmail thread ID.
	ConversationID string `json:"conversationId"`
}

// GetConversationOperation is the getConversation Query.
type GetConversationOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (GetConversationOperation) Definition() sdkgo.QueryDefinition { return GetConversationDefinition }

// Invoke sends GET /v1/inboxes/{inbox_id}/conversations/{conversation_id}.
func (operation GetConversationOperation) Invoke(call sdkgo.Call, input GetConversationInput) sdkgo.QueryAttempt[ConversationDetails] {
	if err := errors.Join(validateHiverID("inboxId", input.InboxID), validateHiverID("conversationId", input.ConversationID)); err != nil {
		return sdkgo.NewQueryBranch(GetConversationBranchDefect, ConversationDetails{}, hiverFailurePointer(sdkgo.FailureValidation, getConversationOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, getConversationOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetConversationBranchDefect, ConversationDetails{}, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call, credentials, getConversationOperation, hiverRequest{
		method: http.MethodGet, path: conversationPath(input.InboxID, input.ConversationID),
	})
	receipt := operation.client.receipt(call, result.response, input.ConversationID)
	if attempt, isTerminal := queryAttemptForExchange[ConversationDetails](result, receipt, queryBranches{
		notFound: GetConversationBranchNotFound, providerRejected: GetConversationBranchProviderRejected,
		invalidResponse: GetConversationBranchInvalidResponse, defect: GetConversationBranchDefect,
	}); isTerminal {
		return attempt
	}
	details, err := decodeConversationBody(result.response.body, input.InboxID)
	if err != nil {
		return sdkgo.NewQueryBranch(GetConversationBranchInvalidResponse, ConversationDetails{}, hiverFailurePointer(sdkgo.FailureProtocol, getConversationOperation, "Hiver returned an invalid conversation: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(GetConversationBranchFound, details, nil, operation.client.receipt(call, result.response, details.Conversation.ID))
}

func decodeConversationBody(body []byte, inboxID string) (ConversationDetails, error) {
	raw, err := decodeSingleData(body)
	if err != nil {
		return ConversationDetails{}, err
	}
	return decodeConversation(raw, inboxID)
}
