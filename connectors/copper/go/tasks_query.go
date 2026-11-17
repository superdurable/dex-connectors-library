// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package copper

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// TaskMetadata contains the public identity and status of one Copper task.
type TaskMetadata struct {
	// ID is the stable Copper task identifier.
	ID int64 `json:"id"`
	// Name is the task display name.
	Name string `json:"name"`
	// Status is the current task status.
	Status string `json:"status"`
}

// GetTaskInput identifies one task by Copper ID.
type GetTaskInput struct {
	// TaskID is a positive decimal identifier.
	TaskID string `json:"taskId"`
}

// GetTaskOperation reads one task.
type GetTaskOperation struct{ client *Client }

// GetTask binds the Query to this client.
func (client *Client) GetTask() GetTaskOperation {
	return GetTaskOperation{client}
}

// Definition returns the generated Query contract.
func (GetTaskOperation) Definition() sdkgo.QueryDefinition { return GetTaskDefinition }

// Invoke reads one task without custom fields.
func (operation GetTaskOperation) Invoke(call sdkgo.Call, input GetTaskInput) sdkgo.QueryAttempt[TaskMetadata] {
	branches := queryBranches{found: GetTaskBranchFound, notFound: GetTaskBranchNotFound, rejected: GetTaskBranchProviderRejected, invalid: GetTaskBranchInvalidResponse, defect: GetTaskBranchDefect}
	if !validTaskID(input.TaskID) {
		return queryBranch("getTask", branches.defect, TaskMetadata{}, sdkgo.FailureValidation, "task ID must be positive")
	}
	return invokeQuery(call, operation.client, "getTask", "/tasks/"+input.TaskID, branches, decodeTask)
}

func decodeTask(body []byte) (TaskMetadata, error) {
	var wire struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TaskMetadata{}, err
	}
	if wire.ID <= 0 || strings.TrimSpace(wire.Name) == "" || strings.TrimSpace(wire.Status) == "" {
		return TaskMetadata{}, fmt.Errorf("task identity missing")
	}
	return TaskMetadata{ID: wire.ID, Name: wire.Name, Status: wire.Status}, nil
}
