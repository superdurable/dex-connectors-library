// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom

import (
	"fmt"
	"sort"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// DefaultLatestPartLimit is the number of parts getConversation returns when LatestPartLimit is zero.
	DefaultLatestPartLimit = 10
	// MaxLatestPartLimit is the largest accepted LatestPartLimit.
	MaxLatestPartLimit = 50

	getConversationOperation = "getConversation"
)

// GetConversationInput names one conversation and how many of its latest parts to return.
type GetConversationInput struct {
	// ConversationID is the Intercom conversation ID, a string of digits.
	ConversationID string `json:"conversationId"`
	// LatestPartLimit is 1 to MaxLatestPartLimit; zero uses DefaultLatestPartLimit.
	LatestPartLimit int `json:"latestPartLimit,omitempty"`
}

// ConversationDetails is one conversation with its first message and latest parts.
type ConversationDetails struct {
	// Conversation is the conversation, with Source.Body set to the first message as plain text.
	Conversation Conversation `json:"conversation"`
	// LatestParts lists the newest parts first: replies, notes, and events such as close or assignment.
	LatestParts []ConversationPart `json:"latestParts"`
	// HasOlderParts reports that the conversation has parts older than LatestParts.
	HasOlderParts bool `json:"hasOlderParts,omitempty"`
	// PartCount is Intercom's count of the conversation's parts.
	PartCount int `json:"partCount"`
}

// GetConversationOperation is the getConversation Query.
type GetConversationOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (GetConversationOperation) Definition() sdkgo.QueryDefinition { return GetConversationDefinition }

// Invoke reads GET /conversations/{id}?display_as=plaintext, which returns up to the 500 most recent
// parts, and keeps the newest LatestPartLimit of them.
func (operation GetConversationOperation) Invoke(call sdkgo.Call, input GetConversationInput) sdkgo.QueryAttempt[ConversationDetails] {
	if err := validateGetConversationInput(input); err != nil {
		return sdkgo.NewQueryBranch(GetConversationBranchDefect, ConversationDetails{}, intercomFailurePointer(sdkgo.FailureValidation, getConversationOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, getConversationOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetConversationBranchDefect, ConversationDetails{}, failure, sdkgo.Receipt{})
	}
	result := operation.client.readConversation(call, credentials, getConversationOperation, input.ConversationID)
	receipt := operation.client.receipt(call, result.response, input.ConversationID)
	switch {
	case result.outcome == exchangeSucceeded:
	case result.isRetryableRead():
		return sdkgo.NewQueryRetry[ConversationDetails](result.failure, result.retryAfter)
	case result.outcome == exchangeNotFound:
		return sdkgo.NewQueryBranch(GetConversationBranchNotFound, ConversationDetails{}, &result.failure, receipt)
	case result.outcome == exchangeInvalid:
		return sdkgo.NewQueryBranch(GetConversationBranchInvalidResponse, ConversationDetails{}, &result.failure, receipt)
	case result.outcome == exchangeDefect:
		return sdkgo.NewQueryBranch(GetConversationBranchDefect, ConversationDetails{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(GetConversationBranchProviderRejected, ConversationDetails{}, &result.failure, receipt)
	}
	partLimit := input.LatestPartLimit
	if partLimit == 0 {
		partLimit = DefaultLatestPartLimit
	}
	details, err := decodeConversationDetails(result.response.body, input.ConversationID, partLimit)
	if err != nil {
		return sdkgo.NewQueryBranch(GetConversationBranchInvalidResponse, ConversationDetails{}, intercomFailurePointer(sdkgo.FailureProtocol, getConversationOperation, "Intercom returned an invalid conversation: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(GetConversationBranchFound, details, nil, receipt)
}

func validateGetConversationInput(input GetConversationInput) error {
	if err := validateConversationID("conversationId", input.ConversationID); err != nil {
		return err
	}
	if input.LatestPartLimit < 0 || input.LatestPartLimit > MaxLatestPartLimit {
		return fmt.Errorf("latestPartLimit must be between 1 and %d, or zero for %d", MaxLatestPartLimit, DefaultLatestPartLimit)
	}
	return nil
}

// decodeConversationDetails orders parts newest first, whatever order Intercom returned them in.
func decodeConversationDetails(body []byte, conversationID string, partLimit int) (ConversationDetails, error) {
	wire, err := decodeConversationEnvelope(body, conversationID)
	if err != nil {
		return ConversationDetails{}, err
	}
	conversation, err := decodeConversationWire(wire, true)
	if err != nil {
		return ConversationDetails{}, err
	}
	parts := make([]ConversationPart, 0, len(wire.ConversationParts.Parts))
	for index, partWire := range wire.ConversationParts.Parts {
		part, err := decodePartWire(partWire)
		if err != nil {
			return ConversationDetails{}, fmt.Errorf("part %d: %w", index, err)
		}
		parts = append(parts, part)
	}
	sortPartsNewestFirst(parts)
	details := ConversationDetails{
		Conversation: conversation, LatestParts: parts[:min(len(parts), partLimit)],
		PartCount: max(wire.ConversationParts.TotalCount, len(parts)),
	}
	details.HasOlderParts = details.PartCount > len(details.LatestParts)
	return details, nil
}

func sortPartsNewestFirst(parts []ConversationPart) {
	sort.SliceStable(parts, func(left, right int) bool {
		if !parts[left].CreatedAt.Equal(parts[right].CreatedAt) {
			return parts[left].CreatedAt.After(parts[right].CreatedAt)
		}
		return comparePartIDs(parts[left].ID, parts[right].ID) > 0
	})
}

// comparePartIDs orders numeric part IDs by value and falls back to text order.
func comparePartIDs(left string, right string) int {
	if len(left) != len(right) && conversationIDPattern.MatchString(left) && conversationIDPattern.MatchString(right) {
		if len(left) > len(right) {
			return 1
		}
		return -1
	}
	switch {
	case left > right:
		return 1
	case left < right:
		return -1
	default:
		return 0
	}
}
