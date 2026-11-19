// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meistertask

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// RemoveTaskLabelInput identifies the attachment to remove.
type RemoveTaskLabelInput struct {
	// TaskLabelID is a positive decimal attachment ID.
	TaskLabelID string `json:"taskLabelId"`
}

// RemovedTaskLabel confirms the requested attachment ID.
type RemovedTaskLabel struct {
	// TaskLabelID is the ID submitted for deletion.
	TaskLabelID string `json:"taskLabelId"`
}

// RemoveTaskLabelOperation removes an attachment by ID.
type RemoveTaskLabelOperation struct{ client *Client }

// RemoveTaskLabel binds the Mutation to this client.
func (client *Client) RemoveTaskLabel() RemoveTaskLabelOperation {
	return RemoveTaskLabelOperation{client}
}

// Definition returns the generated delete Mutation contract.
func (RemoveTaskLabelOperation) Definition() sdkgo.MutationDefinition {
	return RemoveTaskLabelDefinition
}

// IdempotencyKey supplies a stable call identity for deletion.
func (RemoveTaskLabelOperation) IdempotencyKey(callID sdkgo.CallID, _ RemoveTaskLabelInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes one attachment; repeated deletes cannot create a duplicate effect.
func (operation RemoveTaskLabelOperation) Invoke(call sdkgo.Call, input RemoveTaskLabelInput) sdkgo.MutationAttempt[RemovedTaskLabel] {
	if !validTaskID(input.TaskLabelID) {
		return labelMutationBranch("removeTaskLabel", RemoveTaskLabelBranchDefect, RemovedTaskLabel{}, sdkgo.FailureValidation, "attachment ID is invalid")
	}
	response := operation.client.delete(call, "/task_labels/"+input.TaskLabelID)
	if errors.Is(response.err, errCredential) {
		return labelMutationBranch("removeTaskLabel", RemoveTaskLabelBranchDefect, RemovedTaskLabel{}, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[RemovedTaskLabel](labelFailure("removeTaskLabel", sdkgo.FailureTransport, "MeisterTask request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[RemovedTaskLabel](labelFailure("removeTaskLabel", sdkgo.FailureRateLimit, "MeisterTask rate limited the delete"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[RemovedTaskLabel](labelFailure("removeTaskLabel", sdkgo.FailureAvailability, "MeisterTask delete outcome is not confirmed"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return labelMutationBranch("removeTaskLabel", RemoveTaskLabelBranchNotFound, RemovedTaskLabel{}, sdkgo.FailureNotFound, "MeisterTask did not expose the attachment")
	}
	if response.status != http.StatusOK && response.status != http.StatusNoContent {
		return labelMutationBranch("removeTaskLabel", RemoveTaskLabelBranchProviderRejected, RemovedTaskLabel{}, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[RemovedTaskLabel](labelFailure("removeTaskLabel", sdkgo.FailureTransport, "MeisterTask delete response was interrupted"), 0)
	}
	return sdkgo.NewMutationBranch(RemoveTaskLabelBranchRemoved, RemovedTaskLabel{TaskLabelID: input.TaskLabelID}, nil, sdkgo.Receipt{})
}

func labelMutationBranch[Output any](operation string, branch sdkgo.BranchID, value Output, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[Output] {
	failure := labelFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}
