// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package harvest

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListTasksInput selects one cursor-paginated page of tasks.
type ListTasksInput struct {
	// Cursor is the opaque cursor returned by the prior page.
	Cursor string `json:"cursor,omitempty"`
	// PerPage is from 1 to 2000; zero uses 100.
	PerPage int `json:"perPage,omitempty"`
	// IsActive optionally filters by active state.
	IsActive *bool `json:"isActive,omitempty"`
}

// GetTaskInput identifies a task by its Harvest ID.
type GetTaskInput struct {
	ID int64 `json:"id"`
}

// CreateTaskInput creates a Harvest task with optional defaults.
type CreateTaskInput struct {
	// Name is the task label.
	Name string `json:"name"`
	// BillableByDefault optionally sets the billable state for new projects.
	BillableByDefault *bool `json:"billableByDefault,omitempty"`
	// DefaultHourlyRate optionally sets a nonnegative rate in the account currency.
	DefaultHourlyRate *float64 `json:"defaultHourlyRate,omitempty"`
	// IsDefault optionally adds this task to new projects.
	IsDefault *bool `json:"isDefault,omitempty"`
	// IsActive optionally selects active or archived state.
	IsActive *bool `json:"isActive,omitempty"`
}

// UpdateTaskInput replaces supplied task fields.
type UpdateTaskInput struct {
	// ID is the positive Harvest task ID.
	ID int64 `json:"id"`
	// Name replaces the task label when supplied.
	Name *string `json:"name,omitempty"`
	// BillableByDefault replaces the default billable state.
	BillableByDefault *bool `json:"billableByDefault,omitempty"`
	// DefaultHourlyRate replaces the nonnegative rate in the account currency.
	DefaultHourlyRate *float64 `json:"defaultHourlyRate,omitempty"`
	// IsDefault replaces the automatic assignment setting.
	IsDefault *bool `json:"isDefault,omitempty"`
	// IsActive archives or restores the task.
	IsActive *bool `json:"isActive,omitempty"`
}

// DeleteTaskInput identifies a task to delete.
type DeleteTaskInput struct {
	ID int64 `json:"id"`
}

// TaskRecord is a Harvest task and its project defaults.
type TaskRecord struct {
	// ID is assigned by Harvest.
	ID int64 `json:"id"`
	// Name is the task label.
	Name string `json:"name"`
	// BillableByDefault is the default billable state.
	BillableByDefault bool `json:"billableByDefault"`
	// DefaultHourlyRate is the rate in the account currency.
	DefaultHourlyRate float64 `json:"defaultHourlyRate"`
	// IsDefault controls automatic assignment to new projects.
	IsDefault bool `json:"isDefault"`
	// IsActive reports whether the task is active.
	IsActive bool `json:"isActive"`
}

// TaskPage contains a task page and cursor.
type TaskPage struct {
	// Tasks are the records in this page.
	Tasks []TaskRecord `json:"tasks"`
	// NextCursor is blank on the final page.
	NextCursor string `json:"nextCursor,omitempty"`
}

// DeletedTask identifies a deleted task.
type DeletedTask struct {
	ID int64 `json:"id"`
}

// ListTasksOperation reads a task page.
type ListTasksOperation struct{ client *Client }

// ListTasks binds the task-list Query to this client.
func (client *Client) ListTasks() ListTasksOperation { return ListTasksOperation{client} }

// Definition returns the generated Query contract.
func (ListTasksOperation) Definition() sdkgo.QueryDefinition { return ListTasksDefinition }

// Invoke validates pagination and reads tasks.
func (operation ListTasksOperation) Invoke(call sdkgo.Call, input ListTasksInput) sdkgo.QueryAttempt[TaskPage] {
	if err := validateHarvestPage(input.Cursor, input.PerPage); err != nil {
		return failedBranch[TaskPage]("listTasks", ListTasksBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	path := harvestPagePath("/tasks", input.Cursor, input.PerPage)
	if input.IsActive != nil {
		path += "&is_active=" + strconv.FormatBool(*input.IsActive)
	}
	return invokeGet(operation.client, call, "listTasks", path, ListTasksBranchListed, ListTasksBranchProviderRejected, decodeTaskPage)
}

// GetTaskOperation reads a Harvest task.
type GetTaskOperation struct{ client *Client }

// GetTask binds the task lookup Query to this client.
func (client *Client) GetTask() GetTaskOperation { return GetTaskOperation{client} }

// Definition returns the generated Query contract.
func (GetTaskOperation) Definition() sdkgo.QueryDefinition { return GetTaskDefinition }

// Invoke reads one task by ID.
func (operation GetTaskOperation) Invoke(call sdkgo.Call, input GetTaskInput) sdkgo.QueryAttempt[TaskRecord] {
	if input.ID <= 0 {
		return failedBranch[TaskRecord]("getTask", GetTaskBranchDefect, sdkgo.FailureValidation, "task ID must be positive")
	}
	return invokeGet(operation.client, call, "getTask", "/tasks/"+strconv.FormatInt(input.ID, 10), GetTaskBranchFound, GetTaskBranchNotFound, decodeTaskRecord)
}

// CreateTaskOperation creates a task with bounded repeat.
type CreateTaskOperation struct{ client *Client }

// CreateTask binds task creation to this client.
func (client *Client) CreateTask() CreateTaskOperation { return CreateTaskOperation{client} }

// Definition returns the generated Mutation contract.
func (CreateTaskOperation) Definition() sdkgo.MutationDefinition { return CreateTaskDefinition }

// IdempotencyKey records the Dex call identity; Harvest does not deduplicate this POST.
func (CreateTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a task under the limited dispatch fence.
func (operation CreateTaskOperation) Invoke(call sdkgo.Call, input CreateTaskInput) sdkgo.MutationAttempt[TaskRecord] {
	if !validHarvestTaskName(input.Name) || !validHarvestHourlyRate(input.DefaultHourlyRate) {
		return harvestMutationFailure[TaskRecord]("createTask", CreateTaskBranchDefect, sdkgo.FailureValidation, "task name or hourly rate is invalid")
	}
	payload, err := json.Marshal(struct {
		Name              string   `json:"name"`
		BillableByDefault *bool    `json:"billable_by_default,omitempty"`
		DefaultHourlyRate *float64 `json:"default_hourly_rate,omitempty"`
		IsDefault         *bool    `json:"is_default,omitempty"`
		IsActive          *bool    `json:"is_active,omitempty"`
	}{input.Name, input.BillableByDefault, input.DefaultHourlyRate, input.IsDefault, input.IsActive})
	if err != nil {
		return harvestMutationFailure[TaskRecord]("createTask", CreateTaskBranchDefect, sdkgo.FailureValidation, "task input cannot be encoded")
	}
	return invokeLimitedHarvestCreate(operation.client, call, "createTask", CreateTaskDefinition.StepDefaults.ExecuteMethodTimeout, "/tasks", payload, CreateTaskBranchCreated, CreateTaskBranchProviderRejected, CreateTaskBranchDefect, decodeTaskRecord, func(task TaskRecord) string { return strconv.FormatInt(task.ID, 10) })
}

// UpdateTaskOperation changes absolute task fields.
type UpdateTaskOperation struct{ client *Client }

// UpdateTask binds the task update Mutation to this client.
func (client *Client) UpdateTask() UpdateTaskOperation { return UpdateTaskOperation{client} }

// Definition returns the generated Mutation contract.
func (UpdateTaskOperation) Definition() sdkgo.MutationDefinition { return UpdateTaskDefinition }

// IdempotencyKey records the stable Dex call identity.
func (UpdateTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke applies a repeat-safe task PATCH.
func (operation UpdateTaskOperation) Invoke(call sdkgo.Call, input UpdateTaskInput) sdkgo.MutationAttempt[TaskRecord] {
	if input.ID <= 0 || input.Name == nil && input.BillableByDefault == nil && input.DefaultHourlyRate == nil && input.IsDefault == nil && input.IsActive == nil || input.Name != nil && !validHarvestTaskName(*input.Name) || !validHarvestHourlyRate(input.DefaultHourlyRate) {
		return harvestMutationFailure[TaskRecord]("updateTask", UpdateTaskBranchDefect, sdkgo.FailureValidation, "task ID or update fields are invalid")
	}
	payload, err := json.Marshal(struct {
		Name              *string  `json:"name,omitempty"`
		BillableByDefault *bool    `json:"billable_by_default,omitempty"`
		DefaultHourlyRate *float64 `json:"default_hourly_rate,omitempty"`
		IsDefault         *bool    `json:"is_default,omitempty"`
		IsActive          *bool    `json:"is_active,omitempty"`
	}{input.Name, input.BillableByDefault, input.DefaultHourlyRate, input.IsDefault, input.IsActive})
	if err != nil {
		return harvestMutationFailure[TaskRecord]("updateTask", UpdateTaskBranchDefect, sdkgo.FailureValidation, "task update cannot be encoded")
	}
	return invokeSafeHarvestWrite(operation.client, call, "updateTask", http.MethodPatch, "/tasks/"+strconv.FormatInt(input.ID, 10), payload, UpdateTaskBranchUpdated, UpdateTaskBranchNotFound, UpdateTaskBranchProviderRejected, decodeTaskRecord)
}

// DeleteTaskOperation deletes one task.
type DeleteTaskOperation struct{ client *Client }

// DeleteTask binds the task deletion Mutation to this client.
func (client *Client) DeleteTask() DeleteTaskOperation { return DeleteTaskOperation{client} }

// Definition returns the generated Mutation contract.
func (DeleteTaskOperation) Definition() sdkgo.MutationDefinition { return DeleteTaskDefinition }

// IdempotencyKey records the stable Dex call identity.
func (DeleteTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends a repeat-safe task DELETE.
func (operation DeleteTaskOperation) Invoke(call sdkgo.Call, input DeleteTaskInput) sdkgo.MutationAttempt[DeletedTask] {
	if input.ID <= 0 {
		return harvestMutationFailure[DeletedTask]("deleteTask", DeleteTaskBranchDefect, sdkgo.FailureValidation, "task ID must be positive")
	}
	decode := func(_ []byte) (DeletedTask, error) { return DeletedTask{ID: input.ID}, nil }
	return invokeSafeHarvestWrite(operation.client, call, "deleteTask", http.MethodDelete, "/tasks/"+strconv.FormatInt(input.ID, 10), nil, DeleteTaskBranchDeleted, DeleteTaskBranchNotFound, DeleteTaskBranchProviderRejected, decode)
}

func validHarvestTaskName(name string) bool { return strings.TrimSpace(name) != "" && len(name) <= 255 }
func validHarvestHourlyRate(rate *float64) bool {
	return rate == nil || *rate >= 0 && !math.IsNaN(*rate) && !math.IsInf(*rate, 0)
}
func decodeTaskRecord(body []byte) (TaskRecord, error) {
	var wire struct {
		ID                int64   `json:"id"`
		Name              string  `json:"name"`
		BillableByDefault bool    `json:"billable_by_default"`
		DefaultHourlyRate float64 `json:"default_hourly_rate"`
		IsDefault         bool    `json:"is_default"`
		IsActive          bool    `json:"is_active"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TaskRecord{}, err
	}
	if wire.ID <= 0 || wire.Name == "" || wire.DefaultHourlyRate < 0 {
		return TaskRecord{}, fmt.Errorf("missing task identity")
	}
	return TaskRecord{ID: wire.ID, Name: wire.Name, BillableByDefault: wire.BillableByDefault, DefaultHourlyRate: wire.DefaultHourlyRate, IsDefault: wire.IsDefault, IsActive: wire.IsActive}, nil
}
func decodeTaskPage(body []byte) (TaskPage, error) {
	var wire struct {
		Tasks []json.RawMessage `json:"tasks"`
		Links struct {
			Next *string `json:"next"`
		} `json:"links"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TaskPage{}, err
	}
	if wire.Tasks == nil {
		return TaskPage{}, fmt.Errorf("missing tasks")
	}
	next, err := nextHarvestCursor(wire.Links.Next, "/tasks")
	if err != nil {
		return TaskPage{}, err
	}
	page := TaskPage{Tasks: make([]TaskRecord, 0, len(wire.Tasks)), NextCursor: next}
	for _, raw := range wire.Tasks {
		task, err := decodeTaskRecord(raw)
		if err != nil {
			return TaskPage{}, err
		}
		page.Tasks = append(page.Tasks, task)
	}
	return page, nil
}
