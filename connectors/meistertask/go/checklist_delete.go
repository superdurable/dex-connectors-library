// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meistertask

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// DeleteChecklistInput identifies the checklist to remove.
type DeleteChecklistInput struct {
	// ChecklistID is a positive decimal checklist ID.
	ChecklistID string `json:"checklistId"`
}

// DeletedChecklist confirms the requested checklist ID.
type DeletedChecklist struct {
	// ChecklistID is the ID submitted for deletion.
	ChecklistID string `json:"checklistId"`
}

// DeleteChecklistOperation removes an checklist by ID.
type DeleteChecklistOperation struct{ client *Client }

// DeleteChecklist binds the Mutation to this client.
func (client *Client) DeleteChecklist() DeleteChecklistOperation {
	return DeleteChecklistOperation{client}
}

// Definition returns the generated delete Mutation contract.
func (DeleteChecklistOperation) Definition() sdkgo.MutationDefinition {
	return DeleteChecklistDefinition
}

// IdempotencyKey supplies a stable call identity for deletion.
func (DeleteChecklistOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteChecklistInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes one checklist; repeated deletes cannot create a duplicate effect.
func (operation DeleteChecklistOperation) Invoke(call sdkgo.Call, input DeleteChecklistInput) sdkgo.MutationAttempt[DeletedChecklist] {
	if !validTaskID(input.ChecklistID) {
		return checklistMutationBranch("deleteChecklist", DeleteChecklistBranchDefect, DeletedChecklist{}, sdkgo.FailureValidation, "checklist ID is invalid")
	}
	response := operation.client.delete(call, "/checklists/"+input.ChecklistID)
	if errors.Is(response.err, errCredential) {
		return checklistMutationBranch("deleteChecklist", DeleteChecklistBranchDefect, DeletedChecklist{}, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[DeletedChecklist](checklistFailure("deleteChecklist", sdkgo.FailureTransport, "MeisterTask request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[DeletedChecklist](checklistFailure("deleteChecklist", sdkgo.FailureRateLimit, "MeisterTask rate limited the delete"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[DeletedChecklist](checklistFailure("deleteChecklist", sdkgo.FailureAvailability, "MeisterTask delete outcome is not confirmed"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return checklistMutationBranch("deleteChecklist", DeleteChecklistBranchNotFound, DeletedChecklist{}, sdkgo.FailureNotFound, "MeisterTask did not expose the checklist")
	}
	if response.status != http.StatusOK && response.status != http.StatusNoContent {
		return checklistMutationBranch("deleteChecklist", DeleteChecklistBranchProviderRejected, DeletedChecklist{}, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[DeletedChecklist](checklistFailure("deleteChecklist", sdkgo.FailureTransport, "MeisterTask delete response was interrupted"), 0)
	}
	return sdkgo.NewMutationBranch(DeleteChecklistBranchDeleted, DeletedChecklist{ChecklistID: input.ChecklistID}, nil, sdkgo.Receipt{})
}
