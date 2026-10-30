// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// DefaultMessageLimit is the number of messages getConversation returns when MessageLimit is zero.
	DefaultMessageLimit = 10
	// MaxMessageLimit is the largest accepted MessageLimit, one Re:amaze message page.
	MaxMessageLimit = 30

	getConversationOperation = "getConversation"
)

// GetConversationInput names one conversation and how many of its newest messages to return.
type GetConversationInput struct {
	// ConversationID is the conversation's slug, such as knock-knock, as Conversation.ID reports it.
	ConversationID string `json:"conversationId"`
	// MessageLimit is 1 to MaxMessageLimit; zero uses DefaultMessageLimit.
	MessageLimit int `json:"messageLimit,omitempty"`
}

// ConversationDetails is one conversation with its newest messages and internal notes.
type ConversationDetails struct {
	// Conversation is the conversation, including its first message.
	Conversation Conversation `json:"conversation"`
	// Messages lists the conversation's newest messages and internal notes, newest first, as
	// Re:amaze orders them.
	Messages []ConversationMessage `json:"messages"`
	// HasMoreMessages reports that the conversation has older messages than the last one returned.
	HasMoreMessages bool `json:"hasMoreMessages,omitempty"`
}

// GetConversationOperation is the getConversation Query.
type GetConversationOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (GetConversationOperation) Definition() sdkgo.QueryDefinition { return GetConversationDefinition }

// Invoke reads GET /conversations/{slug}, then the first page of GET /conversations/{slug}/messages.
func (operation GetConversationOperation) Invoke(call sdkgo.Call, input GetConversationInput) sdkgo.QueryAttempt[ConversationDetails] {
	if err := validateGetConversationInput(input); err != nil {
		return sdkgo.NewQueryBranch(GetConversationBranchDefect, ConversationDetails{}, reamazeFailurePointer(sdkgo.FailureValidation, getConversationOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, err := operation.client.resolveCredentials(call)
	if err != nil {
		return credentialQueryAttempt[ConversationDetails](getConversationOperation, GetConversationBranchDefect, err)
	}
	conversationResult := operation.client.exchange(call, credentials, getConversationOperation, reamazeRequest{
		method: http.MethodGet, path: conversationPath(input.ConversationID),
	})
	receipt := operation.client.receipt(call, conversationResult.response, input.ConversationID)
	if attempt, isTerminal := getConversationAttemptForExchange(conversationResult, receipt); isTerminal {
		return attempt
	}
	conversation, err := decodeConversationBody(conversationResult.response.body, input.ConversationID)
	if err != nil {
		return sdkgo.NewQueryBranch(GetConversationBranchInvalidResponse, ConversationDetails{}, reamazeFailurePointer(sdkgo.FailureProtocol, getConversationOperation, "Re:amaze returned an invalid conversation: "+err.Error()), receipt)
	}
	messageLimit := input.MessageLimit
	if messageLimit == 0 {
		messageLimit = DefaultMessageLimit
	}
	messageResult := operation.client.exchange(call, credentials, getConversationOperation, reamazeRequest{
		method: http.MethodGet, path: conversationPath(input.ConversationID) + "/messages", query: url.Values{"page": {"1"}},
	})
	receipt = operation.client.receipt(call, messageResult.response, input.ConversationID)
	if attempt, isTerminal := getConversationAttemptForExchange(messageResult, receipt); isTerminal {
		return attempt
	}
	messages, page, err := decodeMessagePage(messageResult.response.body)
	if err != nil {
		return sdkgo.NewQueryBranch(GetConversationBranchInvalidResponse, ConversationDetails{}, reamazeFailurePointer(sdkgo.FailureProtocol, getConversationOperation, "Re:amaze returned an invalid message page: "+err.Error()), receipt)
	}
	details := ConversationDetails{Conversation: conversation, Messages: messages[:min(messageLimit, len(messages))]}
	details.HasMoreMessages = len(messages) > messageLimit || *page.PageCount > 1 || *page.TotalCount > len(details.Messages)
	return sdkgo.NewQueryBranch(GetConversationBranchFound, details, nil, receipt)
}

// getConversationAttemptForExchange returns the terminal attempt for every outcome except success.
func getConversationAttemptForExchange(result reamazeExchange, receipt sdkgo.Receipt) (sdkgo.QueryAttempt[ConversationDetails], bool) {
	switch {
	case result.outcome == exchangeSucceeded:
		return sdkgo.QueryAttempt[ConversationDetails]{}, false
	case result.isRetryableRead():
		return sdkgo.NewQueryRetry[ConversationDetails](result.failure, result.retryAfter), true
	case result.outcome == exchangeNotFound:
		return sdkgo.NewQueryBranch(GetConversationBranchNotFound, ConversationDetails{}, &result.failure, receipt), true
	case result.outcome == exchangeInvalid:
		return sdkgo.NewQueryBranch(GetConversationBranchInvalidResponse, ConversationDetails{}, &result.failure, receipt), true
	case result.outcome == exchangeDefect:
		return sdkgo.NewQueryBranch(GetConversationBranchDefect, ConversationDetails{}, &result.failure, receipt), true
	default:
		return sdkgo.NewQueryBranch(GetConversationBranchProviderRejected, ConversationDetails{}, &result.failure, receipt), true
	}
}

func validateGetConversationInput(input GetConversationInput) error {
	if err := validateConversationID("conversationId", input.ConversationID); err != nil {
		return err
	}
	if input.MessageLimit < 0 || input.MessageLimit > MaxMessageLimit {
		return fmt.Errorf("messageLimit must be between 1 and %d, or zero for %d", MaxMessageLimit, DefaultMessageLimit)
	}
	return nil
}
