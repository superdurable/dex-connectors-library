// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front

import (
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// MaxTagChanges bounds UpdateConversationInput.AddTagIDs and RemoveTagIDs each.
const MaxTagChanges = 25

// UpdateConversationInput sets a conversation's status, assignee, and tags. Every field is optional, but at
// least one change is required. Unset fields are left as they are.
type UpdateConversationInput struct {
	// ConversationID is the Front conversation ID, such as cnv_55c8c149.
	ConversationID string `json:"conversationId"`
	// Status is open, archived, or deleted, in Front's write vocabulary. It cannot be combined with StatusID.
	Status ConversationStatusChange `json:"status,omitempty"`
	// StatusID is a ticket status ID, such as sts_5x, for a company that uses ticketing.
	StatusID string `json:"statusId,omitempty"`
	// AssigneeID assigns the conversation to one teammate, such as tea_2thf.
	AssigneeID string `json:"assigneeId,omitempty"`
	// IsUnassigned removes the assignee; it cannot be combined with AssigneeID.
	IsUnassigned bool `json:"isUnassigned,omitempty"`
	// AddTagIDs are tag IDs to add, such as tag_13o8r1; a tag already on the conversation is kept once.
	AddTagIDs []string `json:"addTagIds,omitempty"`
	// RemoveTagIDs are tag IDs to remove; a tag not on the conversation is ignored.
	RemoveTagIDs []string `json:"removeTagIds,omitempty"`
}

// UpdateConversationOutput is the conversation read back after the change.
type UpdateConversationOutput struct {
	// Conversation is the conversation as Front returned it after every write.
	Conversation Conversation `json:"conversation"`
	// WasAlreadyApplied reports that the conversation already held every requested value, so this attempt
	// wrote nothing; an earlier attempt of the same Step may have written them.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
}

// UpdateConversationOperation implements the updateConversation Mutation. Build it with
// Client.UpdateConversation.
type UpdateConversationOperation struct{ client *Client }

// conversationPatchWire holds only the fields a PATCH changes; a nil assignee_id entry unassigns.
type conversationPatchWire map[string]any

type tagIDsWire struct {
	TagIDs []string `json:"tag_ids"`
}

// Definition returns the immutable updateConversation operation definition.
func (UpdateConversationOperation) Definition() sdkgo.MutationDefinition {
	return UpdateConversationDefinition
}

// IdempotencyKey returns the call ID, recorded in the Receipt only: every write sets a final value, so a
// repeated attempt converges without a provider key.
func (UpdateConversationOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateConversationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads the conversation, sends only the writes whose value differs, and reads it back: PATCH
// /conversations/{id} for the status and assignee, then POST and DELETE /conversations/{id}/tags. A
// lost answer or server error returns Retry, because repeating any of these writes within the Step is
// safe. A write from a duplicate or retried attempt can still land after the Step completes and undo a
// later Step's change to the same field.
func (operation UpdateConversationOperation) Invoke(call sdkgo.Call, input UpdateConversationInput) sdkgo.MutationAttempt[UpdateConversationOutput] {
	operationID := UpdateConversationDefinition.Operation.OperationID
	client := operation.client
	output := UpdateConversationOutput{}
	if err := validateUpdateConversationInput(input); err != nil {
		return sdkgo.NewMutationBranch(UpdateConversationBranchDefect, output, frontFailurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(UpdateConversationBranchDefect, output, failure, sdkgo.Receipt{})
	}
	receipt := client.receipt(call, input.ConversationID)
	conversationPath := "/conversations/" + input.ConversationID
	current, attempt, isTerminal := client.readConversationForUpdate(call, credentials, conversationPath, receipt)
	if isTerminal {
		return attempt
	}
	writes := planConversationWrites(input, current, conversationPath)
	for index, write := range writes {
		result := client.exchange(call.Context, credentials, operationID, write)
		if result.outcome == exchangeNotFound {
			// The read just found the conversation, so a 404 most likely names an unknown teammate, tag, or status.
			result.outcome = exchangeRejected
		}
		if result.outcome != exchangeSucceeded {
			appliedWritesNote := ""
			if index > 0 {
				appliedWritesNote = "; writes sent before it stay applied"
			}
			return classifyUpdateFailure(result, output, receipt, appliedWritesNote)
		}
	}
	if len(writes) == 0 {
		return sdkgo.NewMutationBranch(UpdateConversationBranchUpdated, UpdateConversationOutput{Conversation: current, WasAlreadyApplied: true}, nil, receipt)
	}
	updated, attempt, isTerminal := client.readConversationForUpdate(call, credentials, conversationPath, receipt)
	if isTerminal {
		return attempt
	}
	if len(planConversationWrites(input, updated, conversationPath)) != 0 {
		// Repeating the writes is safe, so Dex retries until Front shows them or the retry window ends.
		return sdkgo.NewMutationRetry[UpdateConversationOutput](frontFailure(sdkgo.FailureAvailability, operationID,
			"Front accepted the change but does not show it on the conversation yet"), 0)
	}
	return sdkgo.NewMutationBranch(UpdateConversationBranchUpdated, UpdateConversationOutput{Conversation: updated}, nil, receipt)
}

// readConversationForUpdate reads the conversation; a merged one selects notFound, because its ID no longer accepts writes.
func (client *Client) readConversationForUpdate(
	call sdkgo.Call, credentials Credentials, conversationPath string, receipt sdkgo.Receipt,
) (Conversation, sdkgo.MutationAttempt[UpdateConversationOutput], bool) {
	operationID := UpdateConversationDefinition.Operation.OperationID
	result := client.exchange(call.Context, credentials, operationID, frontRequest{method: http.MethodGet, path: conversationPath})
	if result.outcome != exchangeSucceeded {
		return Conversation{}, classifyUpdateFailure(result, UpdateConversationOutput{}, receipt, ""), true
	}
	conversation, err := decodeConversation(result.response.body)
	if err == nil && "/conversations/"+conversation.ID != conversationPath {
		err = errors.New("Front returned another conversation")
	}
	if err != nil {
		return Conversation{}, sdkgo.NewMutationBranch(UpdateConversationBranchInvalidResponse, UpdateConversationOutput{},
			frontFailurePointer(sdkgo.FailureProtocol, operationID, "Front returned an invalid conversation: "+err.Error()), receipt), true
	}
	return conversation, sdkgo.MutationAttempt[UpdateConversationOutput]{}, false
}

// classifyUpdateFailure maps a failed request; appliedWritesNote tells the Flow that earlier writes stay applied.
func classifyUpdateFailure(
	result frontExchange, output UpdateConversationOutput, receipt sdkgo.Receipt, appliedWritesNote string,
) sdkgo.MutationAttempt[UpdateConversationOutput] {
	failure := result.failure
	switch result.outcome {
	case exchangeNotSent, exchangeRateLimited, exchangeUnconfirmed:
		return sdkgo.NewMutationRetry[UpdateConversationOutput](failure, result.retryAfter)
	case exchangeMerged:
		if mergedInto := mergedConversationID(result.response.header.Get("Location")); mergedInto != "" {
			failure.Message = fmt.Sprintf("the conversation was merged into %s; update that conversation", mergedInto)
		}
		failure.Message += appliedWritesNote
		return sdkgo.NewMutationBranch(UpdateConversationBranchNotFound, output, &failure, receipt)
	case exchangeNotFound:
		failure.Message += appliedWritesNote
		return sdkgo.NewMutationBranch(UpdateConversationBranchNotFound, output, &failure, receipt)
	case exchangeDefect:
		return sdkgo.NewMutationBranch(UpdateConversationBranchDefect, output, &failure, sdkgo.Receipt{})
	case exchangeInvalid:
		return sdkgo.NewMutationBranch(UpdateConversationBranchInvalidResponse, output, &failure, receipt)
	default:
		failure.Message += appliedWritesNote
		return sdkgo.NewMutationBranch(UpdateConversationBranchProviderRejected, output, &failure, receipt)
	}
}

// planConversationWrites returns the writes still needed to bring current to input, in sending order.
func planConversationWrites(input UpdateConversationInput, current Conversation, conversationPath string) []frontRequest {
	patch := conversationPatchWire{}
	if input.Status != "" && !conversationHasStatus(current, input.Status) {
		patch["status"] = string(input.Status)
	}
	if input.StatusID != "" && current.StatusID != input.StatusID {
		patch["status_id"] = input.StatusID
	}
	switch currentAssigneeID := assigneeID(current); {
	case input.AssigneeID != "" && currentAssigneeID != input.AssigneeID:
		patch["assignee_id"] = input.AssigneeID
	case input.IsUnassigned && currentAssigneeID != "":
		patch["assignee_id"] = nil
	}
	var writes []frontRequest
	if len(patch) != 0 {
		writes = append(writes, frontRequest{method: http.MethodPatch, path: conversationPath, payload: patch})
	}
	var tagsToAdd, tagsToRemove []string
	for _, tagID := range input.AddTagIDs {
		if !hasTag(current, tagID) && !slices.Contains(tagsToAdd, tagID) {
			tagsToAdd = append(tagsToAdd, tagID)
		}
	}
	for _, tagID := range input.RemoveTagIDs {
		if hasTag(current, tagID) && !slices.Contains(tagsToRemove, tagID) {
			tagsToRemove = append(tagsToRemove, tagID)
		}
	}
	if len(tagsToAdd) != 0 {
		writes = append(writes, frontRequest{method: http.MethodPost, path: conversationPath + "/tags", payload: tagIDsWire{TagIDs: tagsToAdd}})
	}
	if len(tagsToRemove) != 0 {
		writes = append(writes, frontRequest{method: http.MethodDelete, path: conversationPath + "/tags", payload: tagIDsWire{TagIDs: tagsToRemove}})
	}
	return writes
}

// conversationHasStatus compares Front's write vocabulary with the status it reports: open reads back as
// assigned or unassigned.
func conversationHasStatus(conversation Conversation, change ConversationStatusChange) bool {
	switch change {
	case ConversationStatusChangeOpen:
		return conversation.Status == ConversationStatusAssigned || conversation.Status == ConversationStatusUnassigned
	case ConversationStatusChangeArchived:
		return conversation.Status == ConversationStatusArchived
	default:
		return conversation.Status == ConversationStatusDeleted
	}
}

func assigneeID(conversation Conversation) string {
	if conversation.Assignee == nil {
		return ""
	}
	return conversation.Assignee.ID
}

func hasTag(conversation Conversation, tagID string) bool {
	return slices.ContainsFunc(conversation.Tags, func(tag Tag) bool { return tag.ID == tagID })
}

func validateUpdateConversationInput(input UpdateConversationInput) error {
	if err := validateResourceID("conversationId", input.ConversationID, conversationIDPattern, "cnv_55c8c149"); err != nil {
		return err
	}
	switch {
	case input.Status != "" && !slices.Contains(ConversationStatusChanges(), input.Status):
		return fmt.Errorf("status must be open, archived, or deleted, not %q", input.Status)
	case input.Status != "" && input.StatusID != "":
		return errors.New("set status or statusId, not both")
	case input.AssigneeID != "" && input.IsUnassigned:
		return errors.New("set assigneeId or isUnassigned, not both")
	case len(input.AddTagIDs) > MaxTagChanges || len(input.RemoveTagIDs) > MaxTagChanges:
		return fmt.Errorf("addTagIds and removeTagIds hold at most %d tag IDs each", MaxTagChanges)
	case input.Status == "" && input.StatusID == "" && input.AssigneeID == "" && !input.IsUnassigned &&
		len(input.AddTagIDs) == 0 && len(input.RemoveTagIDs) == 0:
		return errors.New("set at least one of status, statusId, assigneeId, isUnassigned, addTagIds, or removeTagIds")
	}
	if input.StatusID != "" {
		if err := validateResourceID("statusId", input.StatusID, statusIDPattern, "sts_5x"); err != nil {
			return err
		}
	}
	if input.AssigneeID != "" {
		if err := validateResourceID("assigneeId", input.AssigneeID, teammateIDPattern, "tea_2thf"); err != nil {
			return err
		}
	}
	for _, tagID := range append(slices.Clone(input.AddTagIDs), input.RemoveTagIDs...) {
		if err := validateResourceID("addTagIds and removeTagIds", tagID, tagIDPattern, "tag_13o8r1"); err != nil {
			return err
		}
	}
	for _, tagID := range input.AddTagIDs {
		if slices.Contains(input.RemoveTagIDs, tagID) {
			return fmt.Errorf("tag %s is both added and removed", tagID)
		}
	}
	return nil
}
