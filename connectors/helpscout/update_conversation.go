// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// UpdateConversationInput is one change to an existing conversation. Blank and zero fields are left
// unchanged; at least one change is required.
type UpdateConversationInput struct {
	// ConversationID is the Help Scout conversation ID.
	ConversationID int64 `json:"conversationId"`
	// Status is the new status: active, pending, closed, or spam. Blank keeps the status.
	Status ConversationStatus `json:"status,omitempty"`
	// AssigneeID assigns a Help Scout user or team, which share one ID space. Zero keeps the assignee.
	AssigneeID int64 `json:"assigneeId,omitempty"`
	// IsUnassigned removes the assignee; it cannot be combined with AssigneeID.
	IsUnassigned bool `json:"isUnassigned,omitempty"`
	// AddTags adds tags and keeps the conversation's other tags; a tag that does not exist is created.
	AddTags []string `json:"addTags,omitempty"`
	// RemoveTags removes tags and keeps the conversation's other tags.
	RemoveTags []string `json:"removeTags,omitempty"`
}

// UpdateConversationOutput is the conversation after the change, as Help Scout returns it.
type UpdateConversationOutput struct {
	// Conversation is the conversation read back after the change, or as read when nothing needed writing.
	Conversation Conversation `json:"conversation"`
	// WasAlreadyApplied reports that the conversation already held every requested value, written by an
	// earlier attempt of this Step or by anyone else, so this attempt wrote nothing.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
}

// UpdateConversationOperation implements the updateConversation Mutation. Build it with
// Client.UpdateConversation.
type UpdateConversationOperation struct{ client *Client }

// conversationPatchWire is one JSON Patch operation, which Help Scout accepts as a single object.
type conversationPatchWire struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

type conversationTagsWire struct {
	Tags []string `json:"tags"`
}

// Definition returns the immutable updateConversation operation definition.
func (UpdateConversationOperation) Definition() sdkgo.MutationDefinition {
	return UpdateConversationDefinition
}

// IdempotencyKey returns the call ID, recorded in the Receipt only. Every write sets a final value, so a
// repeated attempt converges instead of needing a provider key.
func (UpdateConversationOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateConversationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads the conversation, sends only the writes whose value differs from what it read, and reads
// the conversation back. A status is written with PATCH replace /status, an assignee with PATCH replace or
// remove /assignTo, and tags with PUT /tags carrying the complete list: the tags read, minus RemoveTags,
// plus AddTags. Every write sets a final value, so a second dispatch of the same Step, or a retry after a
// lost answer, writes the same values again or finds them applied. A tag change made by someone else
// between the read and the PUT is overwritten.
func (operation UpdateConversationOperation) Invoke(call sdkgo.Call, input UpdateConversationInput) sdkgo.MutationAttempt[UpdateConversationOutput] {
	operationID := UpdateConversationDefinition.Operation.OperationID
	client := operation.client
	if err := validateUpdateConversationInput(input); err != nil {
		return sdkgo.NewMutationBranch(UpdateConversationBranchDefect, UpdateConversationOutput{},
			helpScoutFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	credentials, err := client.resolveCredentials(call)
	if err != nil {
		return credentialMutationAttempt(operationID, UpdateConversationBranchDefect, UpdateConversationOutput{}, err)
	}
	current, attempt, isTerminal := client.readConversationForUpdate(call, &credentials, input.ConversationID, UpdateConversationOutput{})
	if isTerminal {
		return attempt
	}
	writes := planConversationWrites(input, current)
	if len(writes) == 0 {
		return sdkgo.NewMutationBranch(UpdateConversationBranchUpdated, UpdateConversationOutput{Conversation: current, WasAlreadyApplied: true},
			nil, client.receipt(call, input.ConversationID, ""))
	}
	branches := failureBranches{
		notFound: UpdateConversationBranchNotFound, providerRejected: UpdateConversationBranchProviderRejected,
		invalidResponse: UpdateConversationBranchInvalidResponse,
	}
	currentOutput := UpdateConversationOutput{Conversation: current}
	for _, write := range writes {
		response, err := client.send(call.Context, call, &credentials, write, nil)
		if attempt, isTerminal := classifyIdempotentMutationExchange(client, call, operationID, credentials, response, err, branches, input.ConversationID, currentOutput); isTerminal {
			return attempt
		}
	}
	updated, attempt, isTerminal := client.readConversationForUpdate(call, &credentials, input.ConversationID, currentOutput)
	if isTerminal {
		return attempt
	}
	return sdkgo.NewMutationBranch(UpdateConversationBranchUpdated, UpdateConversationOutput{Conversation: updated}, nil,
		client.receipt(call, input.ConversationID, ""))
}

// readConversationForUpdate reads the conversation; a merged one selects notFound with the new ID.
func (client *Client) readConversationForUpdate(
	call sdkgo.Call, credentials *Credentials, conversationID int64, value UpdateConversationOutput,
) (Conversation, sdkgo.MutationAttempt[UpdateConversationOutput], bool) {
	operationID := UpdateConversationDefinition.Operation.OperationID
	response, err := client.send(call.Context, call, credentials, helpScoutRequest{method: http.MethodGet, path: conversationPath(conversationID)}, nil)
	if err == nil && response.statusCode == http.StatusMovedPermanently {
		message := "the conversation was merged into another; update that conversation"
		if mergedIntoID, locationErr := parseConversationLocation(response.header.Get("Location")); locationErr == nil {
			message = fmt.Sprintf("the conversation was merged into conversation %d; update that conversation", mergedIntoID)
		}
		return Conversation{}, sdkgo.NewMutationBranch(UpdateConversationBranchNotFound, value,
			helpScoutFailurePointer(operationID, sdkgo.FailureNotFound, message), client.receipt(call, conversationID, "")), true
	}
	branches := failureBranches{
		notFound: UpdateConversationBranchNotFound, providerRejected: UpdateConversationBranchProviderRejected,
		invalidResponse: UpdateConversationBranchInvalidResponse,
	}
	if attempt, isTerminal := classifyIdempotentMutationExchange(client, call, operationID, *credentials, response, err, branches, conversationID, value); isTerminal {
		return Conversation{}, attempt, true
	}
	conversation, err := decodeConversationBody(response.body, conversationID)
	if err != nil {
		return Conversation{}, sdkgo.NewMutationBranch(UpdateConversationBranchInvalidResponse, value,
			helpScoutFailurePointer(operationID, sdkgo.FailureProtocol, "Help Scout returned an invalid conversation: "+err.Error()),
			client.receipt(call, conversationID, "")), true
	}
	return conversation, sdkgo.MutationAttempt[UpdateConversationOutput]{}, false
}

func validateUpdateConversationInput(input UpdateConversationInput) error {
	if input.ConversationID < 1 {
		return errors.New("conversationId must be a positive Help Scout conversation ID")
	}
	if input.Status != "" {
		if err := validateConversationStatus("status", input.Status); err != nil {
			return err
		}
	}
	switch {
	case input.AssigneeID < 0:
		return errors.New("assigneeId must be a positive Help Scout user or team ID, or zero to keep the assignee")
	case input.IsUnassigned && input.AssigneeID != 0:
		return errors.New("isUnassigned cannot be combined with assigneeId")
	}
	if err := errors.Join(validateTags("addTags", input.AddTags), validateTags("removeTags", input.RemoveTags)); err != nil {
		return err
	}
	for _, tag := range input.AddTags {
		if containsTagFold(input.RemoveTags, tag) {
			return fmt.Errorf("tag %q is in both addTags and removeTags", tag)
		}
	}
	hasRequestedChange := input.Status != "" || input.AssigneeID != 0 || input.IsUnassigned || len(input.AddTags) != 0 || len(input.RemoveTags) != 0
	if !hasRequestedChange {
		return errors.New("at least one of status, assigneeId, isUnassigned, addTags, or removeTags is required")
	}
	return nil
}

// planConversationWrites returns only the writes whose value differs from the conversation as read.
func planConversationWrites(input UpdateConversationInput, current Conversation) []helpScoutRequest {
	path := conversationPath(input.ConversationID)
	var writes []helpScoutRequest
	if input.Status != "" && input.Status != current.Status {
		writes = append(writes, helpScoutRequest{method: http.MethodPatch, path: path,
			payload: conversationPatchWire{Op: "replace", Path: "/status", Value: string(input.Status)}})
	}
	switch {
	case input.IsUnassigned && current.Assignee != nil:
		writes = append(writes, helpScoutRequest{method: http.MethodPatch, path: path, payload: conversationPatchWire{Op: "remove", Path: "/assignTo"}})
	case input.AssigneeID != 0 && (current.Assignee == nil || current.Assignee.ID != input.AssigneeID):
		writes = append(writes, helpScoutRequest{method: http.MethodPatch, path: path,
			payload: conversationPatchWire{Op: "replace", Path: "/assignTo", Value: input.AssigneeID}})
	}
	if tags, hasTagChange := planConversationTags(current.Tags, input.AddTags, input.RemoveTags); hasTagChange {
		writes = append(writes, helpScoutRequest{method: http.MethodPut, path: path + "/tags", payload: conversationTagsWire{Tags: tags}})
	}
	return writes
}

// planConversationTags compares names without case, keeping each existing tag's spelling as read.
func planConversationTags(current []string, addTags []string, removeTags []string) ([]string, bool) {
	tags := make([]string, 0, len(current)+len(addTags))
	hasTagChange := false
	for _, tag := range current {
		if containsTagFold(removeTags, tag) {
			hasTagChange = true
			continue
		}
		tags = append(tags, tag)
	}
	for _, tag := range addTags {
		if !containsTagFold(tags, tag) {
			tags, hasTagChange = append(tags, tag), true
		}
	}
	return tags, hasTagChange
}

func containsTagFold(tags []string, tag string) bool {
	for _, candidate := range tags {
		if strings.EqualFold(candidate, tag) {
			return true
		}
	}
	return false
}
