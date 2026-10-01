// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	getTaskOperationID    = "getTask"
	getTaskFailureSubject = "task"
)

// GetTaskInput identifies one task.
type GetTaskInput struct {
	// TaskID is the task's gid, the number in its Asana URL, such as 1204567890123456.
	TaskID string `json:"taskId"`
}

// GetTaskOperation implements the getTask Query.
type GetTaskOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (GetTaskOperation) Definition() sdkgo.QueryDefinition { return GetTaskDefinition }

// Invoke reads one task. Transport failures, 408, 429, and 5xx responses are retried.
func (operation GetTaskOperation) Invoke(call sdkgo.Call, input GetTaskInput) sdkgo.QueryAttempt[Task] {
	client := operation.client
	taskID, err := validateGID(input.TaskID, "taskId")
	if err != nil {
		return sdkgo.NewQueryBranch(GetTaskBranchDefect, Task{}, failurePointer(getTaskOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, startFailure := client.startSession(call, getTaskOperationID)
	if startFailure != nil {
		return sdkgo.NewQueryBranch(GetTaskBranchDefect, Task{}, startFailure, sdkgo.Receipt{})
	}
	defer cancel()
	task, receipt, classification := client.readTask(session, getTaskOperationID, taskID)
	switch classification.outcome {
	case readSucceeded:
		return sdkgo.NewQueryBranch(GetTaskBranchFound, task, nil, receipt)
	case readRetry:
		return sdkgo.NewQueryRetry[Task](classification.failure, classification.retryAfter)
	case readNotFound:
		return sdkgo.NewQueryBranch(GetTaskBranchNotFound, Task{}, &classification.failure, receipt)
	case readDefect:
		return sdkgo.NewQueryBranch(GetTaskBranchDefect, Task{}, &classification.failure, receipt)
	case readInvalid:
		return sdkgo.NewQueryBranch(GetTaskBranchInvalidResponse, Task{}, &classification.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(GetTaskBranchProviderRejected, Task{}, &classification.failure, receipt)
	}
}

// readTask reads one task with its detail fields; getTask and updateTask share it.
func (client *Client) readTask(session *operationSession, operationID string, taskID string) (Task, sdkgo.Receipt, readClassification) {
	result := client.exchange(session, asanaRequest{
		method: http.MethodGet, path: joinPath("tasks", taskID), query: url.Values{"opt_fields": {strings.Join(taskDetailFields, ",")}},
	})
	receipt := client.receipt(session, taskID)
	classification := client.classifyRead(operationID, getTaskFailureSubject, result)
	if classification.outcome != readSucceeded {
		return Task{}, receipt, classification
	}
	task, err := decodeTaskBody(result.response.body)
	if err != nil {
		return Task{}, receipt, readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, "Asana returned an invalid task: "+err.Error())}
	}
	return task, receipt, classification
}

// decodeTaskBody decodes a {"data": task} response with the task's detail fields.
func decodeTaskBody(body []byte) (Task, error) {
	resource, err := decodeData[taskResource](body)
	if err != nil {
		return Task{}, err
	}
	return decodeTask(resource, true)
}
