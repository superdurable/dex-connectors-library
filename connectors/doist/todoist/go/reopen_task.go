// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package todoist

import (
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ReopenTaskInput identifies a completed task to restore.
type ReopenTaskInput struct {
	// TaskID is Todoist's opaque task ID, not a URL or title.
	TaskID string `json:"taskId"`
}

// ReopenedTask identifies the task requested for restoration.
type ReopenedTask struct {
	// TaskID is the restored task's ID.
	TaskID string `json:"taskId"`
}

// ReopenTaskOperation restores a completed task to active status.
type ReopenTaskOperation struct{ client *Client }

// Definition returns the generated mutation contract.
func (ReopenTaskOperation) Definition() sdkgo.MutationDefinition { return ReopenTaskDefinition }

// IdempotencyKey preserves the call identity; the provider endpoint does not accept a key.
func (ReopenTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ ReopenTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reopens the task; unknown outcomes retry because restoration sets an absolute active state.
func (operation ReopenTaskOperation) Invoke(call sdkgo.Call, input ReopenTaskInput) sdkgo.MutationAttempt[ReopenedTask] {
	if !taskIDPattern.MatchString(input.TaskID) {
		return sdkgo.NewMutationBranch(ReopenTaskBranchDefect, ReopenedTask{}, failure("reopenTask", sdkgo.FailureValidation, "taskId must be a non-empty opaque task ID"), sdkgo.Receipt{})
	}
	budget := newAttemptBudget(call.Context, ReopenTaskDefinition.StepDefaults.ExecuteMethodTimeout)
	credentials, result, hasCredentials := operation.client.resolveCredentials(call, "reopenTask")
	if hasCredentials {
		result = operation.client.sendRequest(budget.remainingWindow(), credentials, "reopenTask", http.MethodPost, "/tasks/"+input.TaskID+"/reopen", nil, "")
	}
	switch {
	case result.failure == nil:
		return sdkgo.NewMutationBranch(ReopenTaskBranchReopened, ReopenedTask{TaskID: input.TaskID}, nil, sdkgo.Receipt{})
	case result.isLocalDefect:
		return sdkgo.NewMutationBranch(ReopenTaskBranchDefect, ReopenedTask{}, result.failure, sdkgo.Receipt{})
	case result.isTransient || result.status == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[ReopenedTask](*result.failure, result.retryAfter)
	case result.status == http.StatusNotFound:
		return sdkgo.NewMutationBranch(ReopenTaskBranchNotFound, ReopenedTask{}, result.failure, sdkgo.Receipt{})
	default:
		return sdkgo.NewMutationBranch(ReopenTaskBranchProviderRejected, ReopenedTask{}, result.failure, sdkgo.Receipt{})
	}
}
