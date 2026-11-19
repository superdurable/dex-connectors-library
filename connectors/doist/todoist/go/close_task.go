// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package todoist

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// CloseTaskInput identifies a task to close, including a recurring task.
type CloseTaskInput struct {
	// TaskID is Todoist's opaque task ID, not a task URL or title.
	TaskID string `json:"taskId"`
}

// ClosedTask identifies the task whose close command Todoist confirmed.
type ClosedTask struct {
	// TaskID is the task ID supplied to the close command.
	TaskID string `json:"taskId"`
}

// CloseTaskOperation closes a task with a deduplicated Todoist Sync command.
type CloseTaskOperation struct{ client *Client }

// Definition returns the generated mutation contract.
func (CloseTaskOperation) Definition() sdkgo.MutationDefinition { return CloseTaskDefinition }

// IdempotencyKey uses the stable call ID as Todoist's command UUID.
func (CloseTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ CloseTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke submits item_close; a repeated command UUID never advances a recurring task twice.
func (operation CloseTaskOperation) Invoke(call sdkgo.Call, input CloseTaskInput) sdkgo.MutationAttempt[ClosedTask] {
	if !taskIDPattern.MatchString(input.TaskID) {
		return sdkgo.NewMutationBranch(CloseTaskBranchDefect, ClosedTask{}, failure("closeTask", sdkgo.FailureValidation, "taskId must be a non-empty opaque task ID"), sdkgo.Receipt{})
	}
	command, err := json.Marshal([]struct {
		Type string `json:"type"`
		UUID string `json:"uuid"`
		Args struct {
			ID string `json:"id"`
		} `json:"args"`
	}{{Type: "item_close", UUID: string(call.ID), Args: struct {
		ID string `json:"id"`
	}{ID: input.TaskID}}})
	if err != nil {
		return sdkgo.NewMutationBranch(CloseTaskBranchDefect, ClosedTask{}, failure("closeTask", sdkgo.FailureLocalDefect, "close command could not be encoded"), sdkgo.Receipt{})
	}
	form := url.Values{"commands": {string(command)}}
	budget := newAttemptBudget(call.Context, CloseTaskDefinition.StepDefaults.ExecuteMethodTimeout)
	credentials, result, hasCredentials := operation.client.resolveCredentials(call, "closeTask")
	if hasCredentials {
		result = operation.client.sendFormRequest(budget.remainingWindow(), credentials, "closeTask", "/sync", []byte(form.Encode()))
	}
	switch {
	case result.isLocalDefect:
		return sdkgo.NewMutationBranch(CloseTaskBranchDefect, ClosedTask{}, result.failure, sdkgo.Receipt{})
	case result.isTransient || result.status == http.StatusTooManyRequests || result.status == http.StatusServiceUnavailable:
		return sdkgo.NewMutationRetry[ClosedTask](*result.failure, result.retryAfter)
	case result.failure != nil:
		if result.status == http.StatusNotFound {
			return sdkgo.NewMutationBranch(CloseTaskBranchNotFound, ClosedTask{}, result.failure, sdkgo.Receipt{})
		}
		if result.status >= 500 || result.status >= 200 && result.status < 300 {
			return sdkgo.NewMutationRetry[ClosedTask](*result.failure, result.retryAfter)
		}
		return sdkgo.NewMutationBranch(CloseTaskBranchProviderRejected, ClosedTask{}, result.failure, sdkgo.Receipt{})
	}
	var response struct {
		SyncStatus map[string]json.RawMessage `json:"sync_status"`
	}
	if json.Unmarshal(result.body, &response) != nil {
		return closeTaskProtocolRetry("Todoist returned an invalid Sync response")
	}
	status, found := response.SyncStatus[string(call.ID)]
	if !found {
		return closeTaskProtocolRetry("Todoist omitted the close command status")
	}
	if string(status) == `"ok"` {
		return sdkgo.NewMutationBranch(CloseTaskBranchClosed, ClosedTask{TaskID: input.TaskID}, nil, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "todoist", ProviderObjectID: input.TaskID, ObservedAt: time.Now().UTC()})
	}
	var commandError struct {
		HTTPCode  int    `json:"http_code"`
		Error     string `json:"error"`
		ErrorCode string `json:"error_code"`
	}
	if json.Unmarshal(status, &commandError) != nil {
		return closeTaskProtocolRetry("Todoist returned an invalid close command status")
	}
	if commandError.HTTPCode == http.StatusNotFound || strings.EqualFold(commandError.Error, "ITEM_NOT_FOUND") || strings.EqualFold(commandError.ErrorCode, "ITEM_NOT_FOUND") {
		return sdkgo.NewMutationBranch(CloseTaskBranchNotFound, ClosedTask{}, failure("closeTask", sdkgo.FailureNotFound, "Todoist found no task with that ID"), sdkgo.Receipt{})
	}
	if commandError.HTTPCode == http.StatusBadRequest || commandError.HTTPCode == http.StatusUnauthorized || commandError.HTTPCode == http.StatusForbidden || commandError.HTTPCode == http.StatusConflict {
		return sdkgo.NewMutationBranch(CloseTaskBranchProviderRejected, ClosedTask{}, failure("closeTask", sdkgo.FailureProviderRejection, "Todoist rejected the close command"), sdkgo.Receipt{})
	}
	return closeTaskProtocolRetry("Todoist did not confirm the close command")
}

func closeTaskProtocolRetry(message string) sdkgo.MutationAttempt[ClosedTask] {
	return sdkgo.NewMutationRetry[ClosedTask](*failure("closeTask", sdkgo.FailureProtocol, message), 0)
}
