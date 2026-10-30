// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom

import (
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const updateConversationStateOperation = "updateConversationState"

// UpdateConversationStateInput moves one conversation to an Intercom state as an admin.
type UpdateConversationStateInput struct {
	// ConversationID is the Intercom conversation ID, a string of digits.
	ConversationID string `json:"conversationId"`
	// AdminID is the Intercom admin performing the change, chosen with the adminPicker unit.
	AdminID string `json:"adminId"`
	// State is the requested Intercom state: closed closes, open reopens a closed or snoozed
	// conversation, and snoozed hides it until SnoozedUntil.
	State ConversationState `json:"state"`
	// SnoozedUntil is required for snoozed and blank otherwise: an RFC 3339 instant with an explicit
	// offset, such as 2026-02-01T09:00:00Z, when Intercom reopens the conversation. Intercom stores whole seconds.
	SnoozedUntil string `json:"snoozedUntil,omitempty"`
}

// UpdateConversationStateOutput is the conversation after the change.
type UpdateConversationStateOutput struct {
	// Conversation is the conversation Intercom returned after the change, or as read when nothing needed writing.
	Conversation Conversation `json:"conversation"`
	// WasAlreadyApplied reports that the conversation was already in the requested state, because of an
	// earlier attempt of this Step or another change, so this attempt wrote nothing.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
}

// UpdateConversationStateOperation is the updateConversationState Mutation.
type UpdateConversationStateOperation struct {
	client *Client
}

// conversationStateRequestWire is a close, open, or snooze request to POST /conversations/{id}/parts.
type conversationStateRequestWire struct {
	MessageType  string `json:"message_type"`
	Type         string `json:"type,omitempty"`
	AdminID      string `json:"admin_id"`
	SnoozedUntil int64  `json:"snoozed_until,omitempty"`
}

// Definition returns the immutable connector operation definition.
func (UpdateConversationStateOperation) Definition() sdkgo.MutationDefinition {
	return UpdateConversationStateDefinition
}

// IdempotencyKey uses the stable connector Call ID. The operation is safe to repeat because it compares
// the conversation's state before writing, so the key only correlates the Receipt.
func (UpdateConversationStateOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateConversationStateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads the conversation and writes POST /conversations/{id}/parts only when its state differs.
// When Intercom rejects the write, it reads again and reports the requested state as already applied
// when a concurrent attempt, such as Dex's second dispatch of this Step, reached it first.
func (operation UpdateConversationStateOperation) Invoke(call sdkgo.Call, input UpdateConversationStateInput) sdkgo.MutationAttempt[UpdateConversationStateOutput] {
	snoozedUntil, err := validateUpdateConversationStateInput(input)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateConversationStateBranchDefect, UpdateConversationStateOutput{}, intercomFailurePointer(sdkgo.FailureValidation, updateConversationStateOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, updateConversationStateOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(UpdateConversationStateBranchDefect, UpdateConversationStateOutput{}, failure, sdkgo.Receipt{})
	}
	current, attempt, isTerminal := operation.readCurrentState(call, credentials, input)
	if isTerminal {
		return attempt
	}
	if isInRequestedState(current, input.State, snoozedUntil) {
		return sdkgo.NewMutationBranch(UpdateConversationStateBranchUpdated, UpdateConversationStateOutput{Conversation: current, WasAlreadyApplied: true},
			nil, operation.client.receipt(call, intercomResponse{}, input.ConversationID))
	}
	result := operation.client.exchange(call, credentials, updateConversationStateOperation, intercomRequest{
		method: http.MethodPost, path: conversationPath(input.ConversationID) + "/parts", payload: conversationStateRequest(input, snoozedUntil),
	})
	receipt := operation.client.receipt(call, result.response, input.ConversationID)
	switch {
	case result.outcome == exchangeSucceeded:
	case result.isRetryableRead():
		return sdkgo.NewMutationRetry[UpdateConversationStateOutput](result.failure, result.retryAfter)
	case result.outcome == exchangeNotFound:
		return sdkgo.NewMutationBranch(UpdateConversationStateBranchNotFound, UpdateConversationStateOutput{Conversation: current}, &result.failure, receipt)
	case result.outcome == exchangeInvalid:
		return sdkgo.NewMutationBranch(UpdateConversationStateBranchInvalidResponse, UpdateConversationStateOutput{Conversation: current}, &result.failure, receipt)
	case result.outcome == exchangeDefect:
		return sdkgo.NewMutationBranch(UpdateConversationStateBranchDefect, UpdateConversationStateOutput{Conversation: current}, &result.failure, receipt)
	default:
		return operation.reconcileRejectedWrite(call, credentials, input, snoozedUntil, result, receipt)
	}
	wire, err := decodeConversationEnvelope(result.response.body, input.ConversationID)
	var updated Conversation
	if err == nil {
		updated, err = decodeConversationWire(wire, false)
	}
	if err == nil && !isInRequestedState(updated, input.State, snoozedUntil) {
		err = errors.New("conversation returned after the change is not in the requested state")
	}
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateConversationStateBranchInvalidResponse, UpdateConversationStateOutput{Conversation: current}, intercomFailurePointer(sdkgo.FailureProtocol, updateConversationStateOperation,
			"Intercom returned an invalid conversation: "+err.Error()), receipt)
	}
	return sdkgo.NewMutationBranch(UpdateConversationStateBranchUpdated, UpdateConversationStateOutput{Conversation: updated}, nil, receipt)
}

// readCurrentState returns the conversation as read, or the terminal attempt for a failed read.
func (operation UpdateConversationStateOperation) readCurrentState(
	call sdkgo.Call, credentials Credentials, input UpdateConversationStateInput,
) (Conversation, sdkgo.MutationAttempt[UpdateConversationStateOutput], bool) {
	result := operation.client.readConversation(call, credentials, updateConversationStateOperation, input.ConversationID)
	receipt := operation.client.receipt(call, result.response, input.ConversationID)
	switch {
	case result.outcome == exchangeSucceeded:
	case result.isRetryableRead():
		return Conversation{}, sdkgo.NewMutationRetry[UpdateConversationStateOutput](result.failure, result.retryAfter), true
	case result.outcome == exchangeNotFound:
		return Conversation{}, sdkgo.NewMutationBranch(UpdateConversationStateBranchNotFound, UpdateConversationStateOutput{}, &result.failure, receipt), true
	case result.outcome == exchangeInvalid:
		return Conversation{}, sdkgo.NewMutationBranch(UpdateConversationStateBranchInvalidResponse, UpdateConversationStateOutput{}, &result.failure, receipt), true
	case result.outcome == exchangeDefect:
		return Conversation{}, sdkgo.NewMutationBranch(UpdateConversationStateBranchDefect, UpdateConversationStateOutput{}, &result.failure, receipt), true
	default:
		return Conversation{}, sdkgo.NewMutationBranch(UpdateConversationStateBranchProviderRejected, UpdateConversationStateOutput{}, &result.failure, receipt), true
	}
	wire, err := decodeConversationEnvelope(result.response.body, input.ConversationID)
	var current Conversation
	if err == nil {
		current, err = decodeConversationWire(wire, false)
	}
	if err != nil {
		return Conversation{}, sdkgo.NewMutationBranch(UpdateConversationStateBranchInvalidResponse, UpdateConversationStateOutput{}, intercomFailurePointer(sdkgo.FailureProtocol, updateConversationStateOperation,
			"Intercom returned an invalid conversation: "+err.Error()), receipt), true
	}
	return current, sdkgo.MutationAttempt[UpdateConversationStateOutput]{}, false
}

// reconcileRejectedWrite reports a rejection only when the conversation is still not in the requested state.
func (operation UpdateConversationStateOperation) reconcileRejectedWrite(
	call sdkgo.Call, credentials Credentials, input UpdateConversationStateInput, snoozedUntil int64,
	rejection intercomExchange, receipt sdkgo.Receipt,
) sdkgo.MutationAttempt[UpdateConversationStateOutput] {
	current, attempt, isTerminal := operation.readCurrentState(call, credentials, input)
	if isTerminal {
		return attempt
	}
	if isInRequestedState(current, input.State, snoozedUntil) {
		return sdkgo.NewMutationBranch(UpdateConversationStateBranchUpdated, UpdateConversationStateOutput{Conversation: current, WasAlreadyApplied: true}, nil, receipt)
	}
	return sdkgo.NewMutationBranch(UpdateConversationStateBranchProviderRejected, UpdateConversationStateOutput{Conversation: current}, &rejection.failure, receipt)
}

// validateUpdateConversationStateInput returns the snooze time in Unix seconds, or zero for open and closed.
func validateUpdateConversationStateInput(input UpdateConversationStateInput) (int64, error) {
	if err := validateConversationID("conversationId", input.ConversationID); err != nil {
		return 0, err
	}
	if err := validateAdminID(input.AdminID); err != nil {
		return 0, err
	}
	if err := validateConversationState(input.State); err != nil {
		return 0, err
	}
	if input.State != ConversationStateSnoozed {
		if input.SnoozedUntil != "" {
			return 0, errors.New("snoozedUntil is only used with state snoozed")
		}
		return 0, nil
	}
	snoozedUntil, err := time.Parse(time.RFC3339, input.SnoozedUntil)
	if err != nil || snoozedUntil.Unix() <= 0 {
		return 0, errors.New("snoozedUntil must be an RFC 3339 instant with an explicit offset, such as 2026-02-01T09:00:00Z, for state snoozed")
	}
	return snoozedUntil.Unix(), nil
}

// isInRequestedState compares Intercom's state, and for a snooze its reopening time in whole seconds.
func isInRequestedState(conversation Conversation, state ConversationState, snoozedUntil int64) bool {
	if conversation.State != state {
		return false
	}
	if state != ConversationStateSnoozed {
		return true
	}
	return conversation.SnoozedUntil != nil && conversation.SnoozedUntil.Unix() == snoozedUntil
}

func conversationStateRequest(input UpdateConversationStateInput, snoozedUntil int64) conversationStateRequestWire {
	switch input.State {
	case ConversationStateClosed:
		return conversationStateRequestWire{MessageType: "close", Type: "admin", AdminID: input.AdminID}
	case ConversationStateSnoozed:
		return conversationStateRequestWire{MessageType: "snoozed", AdminID: input.AdminID, SnoozedUntil: snoozedUntil}
	default:
		return conversationStateRequestWire{MessageType: "open", AdminID: input.AdminID}
	}
}
