// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const updateConversationOperation = "updateConversation"

// UpdateConversationInput is one change to an existing conversation. Nil and empty fields are
// left unchanged; this release cannot unassign a conversation.
type UpdateConversationInput struct {
	// ConversationID is the conversation's slug, such as knock-knock.
	ConversationID string `json:"conversationId"`
	// Status is the new Re:amaze status, or nil to keep it. Status 0 (Open) is a real value, so
	// this field is a pointer.
	Status *ConversationStatus `json:"status,omitempty"`
	// HoldUntil is when an On Hold (5) conversation's reminder fires, an RFC 3339 instant. It
	// requires Status 5 and is always written, because Re:amaze does not return it to compare.
	HoldUntil string `json:"holdUntil,omitempty"`
	// AssigneeEmail assigns the staff user with this email, or blank to keep the assignee.
	AssigneeEmail string `json:"assigneeEmail,omitempty"`
	// AddTags adds tags and keeps the conversation's other tags. A tag already present in any
	// letter case is kept as is.
	AddTags []string `json:"addTags,omitempty"`
	// RemoveTags removes tags in any letter case and keeps the conversation's other tags.
	RemoveTags []string `json:"removeTags,omitempty"`
}

// UpdateConversationOutput is the conversation after the change.
type UpdateConversationOutput struct {
	// Conversation is the conversation Re:amaze returned after the write, or as read when nothing
	// needed writing.
	Conversation Conversation `json:"conversation"`
	// WasAlreadyApplied reports that the conversation already held every requested value, for
	// example because an earlier attempt of this Step wrote them, so this attempt wrote nothing.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
}

// UpdateConversationOperation is the updateConversation Mutation.
type UpdateConversationOperation struct {
	client *Client
}

type updateConversationRequestWire struct {
	Conversation updateConversationWire `json:"conversation"`
}

type updateConversationWire struct {
	Status    *int                 `json:"status,omitempty"`
	HoldUntil string               `json:"hold_until,omitempty"`
	Assignee  *assigneeRequestWire `json:"assignee,omitempty"`
	TagList   *[]string            `json:"tag_list,omitempty"`
}

type assigneeRequestWire struct {
	Email string `json:"email"`
}

// Definition returns the immutable connector operation definition.
func (UpdateConversationOperation) Definition() sdkgo.MutationDefinition {
	return UpdateConversationDefinition
}

// IdempotencyKey uses the stable connector Call ID. Re:amaze documents no idempotency key; the
// write is safe to repeat because it sets absolute values, so the key only correlates the Receipt.
func (UpdateConversationOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateConversationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads the conversation, then sends PUT /conversations/{slug} with only the fields that
// differ, tags as the complete resulting tag_list. A retried or concurrently dispatched attempt
// sends the same absolute values, or finds them applied and writes nothing.
func (operation UpdateConversationOperation) Invoke(call sdkgo.Call, input UpdateConversationInput) sdkgo.MutationAttempt[UpdateConversationOutput] {
	if err := validateUpdateConversationInput(input); err != nil {
		return sdkgo.NewMutationBranch(UpdateConversationBranchDefect, UpdateConversationOutput{}, reamazeFailurePointer(sdkgo.FailureValidation, updateConversationOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, err := operation.client.resolveCredentials(call)
	if err != nil {
		return credentialMutationAttempt(updateConversationOperation, UpdateConversationBranchDefect, UpdateConversationOutput{}, err)
	}
	readResult := operation.client.exchange(call, credentials, updateConversationOperation, reamazeRequest{method: http.MethodGet, path: conversationPath(input.ConversationID)})
	receipt := operation.client.receipt(call, readResult.response, input.ConversationID)
	if attempt, isTerminal := updateConversationAttemptForExchange(readResult, receipt, UpdateConversationOutput{}); isTerminal {
		return attempt
	}
	current, err := decodeConversationBody(readResult.response.body, input.ConversationID)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateConversationBranchInvalidResponse, UpdateConversationOutput{}, reamazeFailurePointer(sdkgo.FailureProtocol, updateConversationOperation, "Re:amaze returned an invalid conversation: "+err.Error()), receipt)
	}
	currentOutput := UpdateConversationOutput{Conversation: current}
	change, hasEffect := planConversationChange(input, current)
	if !hasEffect {
		return sdkgo.NewMutationBranch(UpdateConversationBranchUpdated, UpdateConversationOutput{Conversation: current, WasAlreadyApplied: true}, nil, receipt)
	}
	writeResult := operation.client.exchange(call, credentials, updateConversationOperation, reamazeRequest{
		method: http.MethodPut, path: conversationPath(input.ConversationID), payload: updateConversationRequestWire{Conversation: change},
	})
	receipt = operation.client.receipt(call, writeResult.response, input.ConversationID)
	if attempt, isTerminal := updateConversationAttemptForExchange(writeResult, receipt, currentOutput); isTerminal {
		return attempt
	}
	updated, err := decodeConversationBody(writeResult.response.body, input.ConversationID)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateConversationBranchInvalidResponse, currentOutput, reamazeFailurePointer(sdkgo.FailureProtocol, updateConversationOperation, "Re:amaze returned an invalid updated conversation: "+err.Error()), receipt)
	}
	return sdkgo.NewMutationBranch(UpdateConversationBranchUpdated, UpdateConversationOutput{Conversation: updated}, nil, receipt)
}

// updateConversationAttemptForExchange returns the terminal attempt for every outcome except success.
func updateConversationAttemptForExchange(result reamazeExchange, receipt sdkgo.Receipt, value UpdateConversationOutput) (sdkgo.MutationAttempt[UpdateConversationOutput], bool) {
	switch {
	case result.outcome == exchangeSucceeded:
		return sdkgo.MutationAttempt[UpdateConversationOutput]{}, false
	case result.isRetryableRead():
		return sdkgo.NewMutationRetry[UpdateConversationOutput](result.failure, result.retryAfter), true
	case result.outcome == exchangeNotFound:
		return sdkgo.NewMutationBranch(UpdateConversationBranchNotFound, value, &result.failure, receipt), true
	case result.outcome == exchangeInvalid:
		return sdkgo.NewMutationBranch(UpdateConversationBranchInvalidResponse, value, &result.failure, receipt), true
	case result.outcome == exchangeDefect:
		return sdkgo.NewMutationBranch(UpdateConversationBranchDefect, value, &result.failure, receipt), true
	default:
		return sdkgo.NewMutationBranch(UpdateConversationBranchProviderRejected, value, &result.failure, receipt), true
	}
}

func validateUpdateConversationInput(input UpdateConversationInput) error {
	if err := validateConversationID("conversationId", input.ConversationID); err != nil {
		return err
	}
	if input.Status != nil {
		if err := validateConversationStatus("status", *input.Status); err != nil {
			return err
		}
	}
	if err := validateHoldUntil(input.HoldUntil, input.Status); err != nil {
		return err
	}
	if input.AssigneeEmail != "" && !isBareEmailAddress(input.AssigneeEmail) {
		return errors.New("assigneeEmail must be one bare staff email address such as agent@example.com")
	}
	if err := errors.Join(validateTags("addTags", input.AddTags), validateTags("removeTags", input.RemoveTags)); err != nil {
		return err
	}
	for _, tag := range input.AddTags {
		if containsTagIgnoringCase(input.RemoveTags, tag) {
			return fmt.Errorf("tag %q is in both addTags and removeTags", tag)
		}
	}
	if input.Status == nil && input.AssigneeEmail == "" && len(input.AddTags) == 0 && len(input.RemoveTags) == 0 {
		return errors.New("at least one of status, assigneeEmail, addTags, or removeTags is required")
	}
	return nil
}

// planConversationChange sends only fields that differ from the conversation as read; no difference means already applied.
func planConversationChange(input UpdateConversationInput, current Conversation) (updateConversationWire, bool) {
	change := updateConversationWire{}
	hasEffect := false
	if input.Status != nil && (*input.Status != current.Status || input.HoldUntil != "") {
		status := int(*input.Status)
		change.Status, change.HoldUntil, hasEffect = &status, input.HoldUntil, true
	}
	if input.AssigneeEmail != "" && (current.Assignee == nil || !strings.EqualFold(current.Assignee.Email, input.AssigneeEmail)) {
		change.Assignee, hasEffect = &assigneeRequestWire{Email: input.AssigneeEmail}, true
	}
	tags := make([]string, 0, len(current.Tags)+len(input.AddTags))
	for _, tag := range current.Tags {
		if !containsTagIgnoringCase(input.RemoveTags, tag) {
			tags = append(tags, tag)
		}
	}
	for _, tag := range input.AddTags {
		if !containsTagIgnoringCase(tags, tag) {
			tags = append(tags, tag)
		}
	}
	if !slices.Equal(tags, current.Tags) {
		change.TagList, hasEffect = &tags, true
	}
	return change, hasEffect
}

func containsTagIgnoringCase(tags []string, tag string) bool {
	return slices.ContainsFunc(tags, func(candidate string) bool { return strings.EqualFold(candidate, tag) })
}
