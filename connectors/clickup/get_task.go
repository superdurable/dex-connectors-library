// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const getTaskOperation = "getTask"

// GetTaskInput names one task.
type GetTaskInput struct {
	// TaskID is ClickUp's task ID, the code after /t/ in the task's web address, or a custom task ID
	// such as DEV-123 when IsCustomTaskID is set.
	TaskID string `json:"taskId"`
	// IsCustomTaskID reads TaskID as the Workspace's custom task ID; it requires WorkspaceID.
	IsCustomTaskID bool `json:"isCustomTaskId,omitempty"`
	// WorkspaceID is the Workspace of a custom task ID; leave it empty for an ordinary task ID.
	WorkspaceID string `json:"workspaceId,omitempty"`
}

// GetTaskOperation is the getTask Query.
type GetTaskOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (GetTaskOperation) Definition() sdkgo.QueryDefinition { return GetTaskDefinition }

// Invoke reads GET /task/{task_id} with its Markdown description and custom fields.
func (operation GetTaskOperation) Invoke(call sdkgo.Call, input GetTaskInput) sdkgo.QueryAttempt[Task] {
	if err := validateGetTaskInput(input); err != nil {
		return sdkgo.NewQueryBranch(GetTaskBranchDefect, Task{}, clickupFailurePointer(sdkgo.FailureValidation, getTaskOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, getTaskOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetTaskBranchDefect, Task{}, failure, sdkgo.Receipt{})
	}
	query := url.Values{"include_markdown_description": {"true"}}
	if input.IsCustomTaskID {
		query.Set("custom_task_ids", "true")
		query.Set("team_id", input.WorkspaceID)
	}
	result := operation.client.exchange(call.Context, credentials, getTaskOperation, clickupRequest{
		method: http.MethodGet, path: taskPath(input.TaskID), query: query,
	})
	receipt := operation.client.receipt(call, input.TaskID)
	switch {
	case result.outcome == exchangeSucceeded:
	case result.isRetryableRead():
		return sdkgo.NewQueryRetry[Task](result.failure, result.retryAfter)
	case result.outcome == exchangeNotFound:
		return sdkgo.NewQueryBranch(GetTaskBranchNotFound, Task{}, &result.failure, receipt)
	case result.outcome == exchangeInvalid:
		return sdkgo.NewQueryBranch(GetTaskBranchInvalidResponse, Task{}, &result.failure, receipt)
	case result.outcome == exchangeDefect:
		return sdkgo.NewQueryBranch(GetTaskBranchDefect, Task{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(GetTaskBranchProviderRejected, Task{}, &result.failure, receipt)
	}
	task, err := decodeTaskBody(result.response.body, true)
	if err != nil {
		return sdkgo.NewQueryBranch(GetTaskBranchInvalidResponse, Task{}, clickupFailurePointer(sdkgo.FailureProtocol, getTaskOperation,
			"ClickUp returned an invalid task: "+err.Error()), receipt)
	}
	receipt.ProviderObjectID = task.ID
	return sdkgo.NewQueryBranch(GetTaskBranchFound, task, nil, receipt)
}

func validateGetTaskInput(input GetTaskInput) error {
	if err := validateTaskID("taskId", input.TaskID); err != nil {
		return err
	}
	if input.IsCustomTaskID {
		return validateNumericID("workspaceId", input.WorkspaceID)
	}
	if input.WorkspaceID != "" {
		return errors.New("workspaceId is only used with isCustomTaskId")
	}
	return nil
}

func taskPath(taskID string) string {
	return "/task/" + url.PathEscape(taskID)
}
