// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meistertask

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// DeleteChecklistItemInput identifies the checklist to remove.
type DeleteChecklistItemInput struct {
	// ChecklistItemID is a positive decimal checklist ID.
	ChecklistItemID string `json:"checklistItemId"`
}

// DeletedChecklistItem confirms the requested checklist ID.
type DeletedChecklistItem struct {
	// ChecklistItemID is the ID submitted for deletion.
	ChecklistItemID string `json:"checklistItemId"`
}

// DeleteChecklistItemOperation removes an checklist by ID.
type DeleteChecklistItemOperation struct{ client *Client }

// DeleteChecklistItem binds the Mutation to this client.
func (client *Client) DeleteChecklistItem() DeleteChecklistItemOperation {
	return DeleteChecklistItemOperation{client}
}

// Definition returns the generated delete Mutation contract.
func (DeleteChecklistItemOperation) Definition() sdkgo.MutationDefinition {
	return DeleteChecklistItemDefinition
}

// IdempotencyKey supplies a stable call identity for deletion.
func (DeleteChecklistItemOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteChecklistItemInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes one checklist; repeated deletes cannot create a duplicate effect.
func (operation DeleteChecklistItemOperation) Invoke(call sdkgo.Call, input DeleteChecklistItemInput) sdkgo.MutationAttempt[DeletedChecklistItem] {
	if !validTaskID(input.ChecklistItemID) {
		return checklistItemMutationBranch("deleteChecklistItem", DeleteChecklistItemBranchDefect, DeletedChecklistItem{}, sdkgo.FailureValidation, "checklist ID is invalid")
	}
	response := operation.client.delete(call, "/checklist_items/"+input.ChecklistItemID)
	if errors.Is(response.err, errCredential) {
		return checklistItemMutationBranch("deleteChecklistItem", DeleteChecklistItemBranchDefect, DeletedChecklistItem{}, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[DeletedChecklistItem](checklistItemFailure("deleteChecklistItem", sdkgo.FailureTransport, "MeisterTask request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[DeletedChecklistItem](checklistItemFailure("deleteChecklistItem", sdkgo.FailureRateLimit, "MeisterTask rate limited the delete"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[DeletedChecklistItem](checklistItemFailure("deleteChecklistItem", sdkgo.FailureAvailability, "MeisterTask delete outcome is not confirmed"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return checklistItemMutationBranch("deleteChecklistItem", DeleteChecklistItemBranchNotFound, DeletedChecklistItem{}, sdkgo.FailureNotFound, "MeisterTask did not expose the checklist")
	}
	if response.status != http.StatusOK && response.status != http.StatusNoContent {
		return checklistItemMutationBranch("deleteChecklistItem", DeleteChecklistItemBranchProviderRejected, DeletedChecklistItem{}, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[DeletedChecklistItem](checklistItemFailure("deleteChecklistItem", sdkgo.FailureTransport, "MeisterTask delete response was interrupted"), 0)
	}
	return sdkgo.NewMutationBranch(DeleteChecklistItemBranchDeleted, DeletedChecklistItem{ChecklistItemID: input.ChecklistItemID}, nil, sdkgo.Receipt{})
}
