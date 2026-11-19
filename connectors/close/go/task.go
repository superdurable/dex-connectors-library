// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package close

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// Task identifies a Close lead task and its completion state.
type Task struct {
	// ID is the provider-assigned task identifier.
	ID string `json:"id"`
	// LeadID identifies the parent lead.
	LeadID string `json:"leadId"`
	// Text is the task's description.
	Text string `json:"text"`
	// IsComplete reports whether the task has been completed.
	IsComplete bool `json:"isComplete"`
}

// TaskPage contains a bounded page of Close tasks.
type TaskPage struct {
	// Tasks contains the returned task records.
	Tasks []Task `json:"tasks"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"hasMore"`
}

// ListTasksInput selects a page of lead tasks.
type ListTasksInput struct {
	// LeadID identifies the parent lead.
	LeadID string `json:"leadId"`
	// Skip is the zero-based offset.
	Skip int `json:"skip,omitempty"`
	// Limit caps records; zero uses 100.
	Limit int `json:"limit,omitempty"`
}

// GetTaskInput identifies a single Close task.
type GetTaskInput struct {
	// TaskID is the Close task_ identifier.
	TaskID string `json:"taskId"`
}

// CreateTaskInput supplies fields for a new lead task.
type CreateTaskInput struct {
	// LeadID identifies the existing lead.
	LeadID string `json:"leadId"`
	// Text is a nonempty task description.
	Text string `json:"text"`
	// AssignedTo optionally identifies a Close user; blank uses the connection user.
	AssignedTo string `json:"assignedTo,omitempty"`
	// Date optionally sets an ISO 8601 due date as YYYY-MM-DD; blank uses Close's default.
	Date string `json:"date,omitempty"`
}

// UpdateTaskInput replaces selected task fields.
type UpdateTaskInput struct {
	// TaskID identifies the task to change.
	TaskID string `json:"taskId"`
	// Text replaces the task description when present.
	Text *string `json:"text,omitempty"`
	// IsComplete replaces the completion state when present.
	IsComplete *bool `json:"isComplete,omitempty"`
	// Date replaces the due date when present as YYYY-MM-DD.
	Date *string `json:"date,omitempty"`
	// AssignedTo replaces the assignee with a Close user ID when present.
	AssignedTo *string `json:"assignedTo,omitempty"`
}

// DeleteTaskInput identifies one task to remove.
type DeleteTaskInput struct {
	// TaskID identifies the task to remove.
	TaskID string `json:"taskId"`
}

// DeletedTask confirms that a task is absent.
type DeletedTask struct {
	// TaskID identifies the absent task.
	TaskID string `json:"taskId"`
}

// ListTasksOperation reads lead tasks.
type ListTasksOperation struct{ client *Client }

// ListTasks binds this Query to the client.
func (client *Client) ListTasks() ListTasksOperation { return ListTasksOperation{client} }

// Definition returns the generated listTasks contract.
func (ListTasksOperation) Definition() sdkgo.QueryDefinition { return ListTasksDefinition }

// Invoke validates pagination and classifies the Close response.
func (operation ListTasksOperation) Invoke(call sdkgo.Call, input ListTasksInput) sdkgo.QueryAttempt[TaskPage] {
	if !validLeadID(input.LeadID) || input.Skip < 0 || input.Limit < 0 || input.Limit > 1000 {
		return failedBranch[TaskPage]("listTasks", ListTasksBranchDefect, sdkgo.FailureValidation, "leadId, skip, or limit is invalid")
	}
	limit := input.Limit
	if limit == 0 {
		limit = 100
	}
	query := url.Values{"lead_id": {input.LeadID}, "_type": {"lead"}, "_skip": {strconv.Itoa(input.Skip)}, "_limit": {strconv.Itoa(limit)}}
	return invokeGet(operation.client, call, "listTasks", "/task/?"+query.Encode(), ListTasksBranchListed, ListTasksBranchProviderRejected, decodeTaskPage)
}

// GetTaskOperation reads one task.
type GetTaskOperation struct{ client *Client }

// GetTask binds this Query to the client.
func (client *Client) GetTask() GetTaskOperation { return GetTaskOperation{client} }

// Definition returns the generated getTask contract.
func (GetTaskOperation) Definition() sdkgo.QueryDefinition { return GetTaskDefinition }

// Invoke validates the ID and classifies the Close response.
func (operation GetTaskOperation) Invoke(call sdkgo.Call, input GetTaskInput) sdkgo.QueryAttempt[Task] {
	if !validTaskID(input.TaskID) {
		return failedBranch[Task]("getTask", GetTaskBranchDefect, sdkgo.FailureValidation, "taskId is invalid")
	}
	return invokeGet(operation.client, call, "getTask", "/task/"+input.TaskID+"/", GetTaskBranchFound, GetTaskBranchNotFound, decodeTask)
}

// CreateTaskOperation creates a lead task with bounded possible sends.
type CreateTaskOperation struct{ client *Client }

// CreateTask binds this Mutation to the client.
func (client *Client) CreateTask() CreateTaskOperation { return CreateTaskOperation{client} }

// Definition returns the generated createTask contract.
func (CreateTaskOperation) Definition() sdkgo.MutationDefinition { return CreateTaskDefinition }

// IdempotencyKey correlates attempts; Close does not deduplicate this key.
func (CreateTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a lead task and reconciles a known provider ID before resending.
func (operation CreateTaskOperation) Invoke(call sdkgo.Call, input CreateTaskInput) sdkgo.MutationAttempt[Task] {
	if !validLeadID(input.LeadID) || strings.TrimSpace(input.Text) == "" || len(input.Text) > 10000 ||
		(input.AssignedTo != "" && !validCloseUserID(input.AssignedTo)) || !validOptionalTaskDate(input.Date) {
		return closeMutationBranch[Task]("createTask", CreateTaskBranchDefect, sdkgo.FailureValidation, "leadId, text, assignedTo, or date is invalid")
	}
	requestBody, err := json.Marshal(struct {
		Type       string `json:"_type"`
		LeadID     string `json:"lead_id"`
		Text       string `json:"text"`
		AssignedTo string `json:"assigned_to,omitempty"`
		Date       string `json:"date,omitempty"`
	}{"lead", input.LeadID, input.Text, input.AssignedTo, input.Date})
	if err != nil {
		return closeMutationBranch[Task]("createTask", CreateTaskBranchDefect, sdkgo.FailureLocalDefect, "task request could not be encoded")
	}
	return invokeCloseCreate(operation.client, call, "createTask", "/task/", requestBody, CreateTaskBranchCreated, CreateTaskBranchProviderRejected, CreateTaskBranchDefect, decodeTask, validTaskID)
}

// UpdateTaskOperation replaces selected task fields.
type UpdateTaskOperation struct{ client *Client }

// UpdateTask binds this Mutation to the client.
func (client *Client) UpdateTask() UpdateTaskOperation { return UpdateTaskOperation{client} }

// Definition returns the generated updateTask contract.
func (UpdateTaskOperation) Definition() sdkgo.MutationDefinition { return UpdateTaskDefinition }

// IdempotencyKey correlates attempts; absolute field replacement is repeat safe.
func (UpdateTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces selected fields and classifies the Close response.
func (operation UpdateTaskOperation) Invoke(call sdkgo.Call, input UpdateTaskInput) sdkgo.MutationAttempt[Task] {
	if !validTaskID(input.TaskID) || (input.Text == nil && input.IsComplete == nil && input.Date == nil && input.AssignedTo == nil) ||
		(input.Text != nil && (strings.TrimSpace(*input.Text) == "" || len(*input.Text) > 10000)) ||
		(input.Date != nil && !validOptionalTaskDate(*input.Date)) ||
		(input.AssignedTo != nil && !validCloseUserID(*input.AssignedTo)) {
		return closeMutationBranch[Task]("updateTask", UpdateTaskBranchDefect, sdkgo.FailureValidation, "taskId and at least one valid field are required")
	}
	requestBody, err := json.Marshal(struct {
		Text       *string `json:"text,omitempty"`
		IsComplete *bool   `json:"is_complete,omitempty"`
		Date       *string `json:"date,omitempty"`
		AssignedTo *string `json:"assigned_to,omitempty"`
	}{input.Text, input.IsComplete, input.Date, input.AssignedTo})
	if err != nil {
		return closeMutationBranch[Task]("updateTask", UpdateTaskBranchDefect, sdkgo.FailureLocalDefect, "task update could not be encoded")
	}
	return invokeCloseWrite(operation.client, call, "updateTask", http.MethodPut, "/task/"+input.TaskID+"/", requestBody, UpdateTaskBranchUpdated, UpdateTaskBranchNotFound, decodeTask)
}

// DeleteTaskOperation removes one task.
type DeleteTaskOperation struct{ client *Client }

// DeleteTask binds this Mutation to the client.
func (client *Client) DeleteTask() DeleteTaskOperation { return DeleteTaskOperation{client} }

// Definition returns the generated deleteTask contract.
func (DeleteTaskOperation) Definition() sdkgo.MutationDefinition { return DeleteTaskDefinition }

// IdempotencyKey correlates attempts; deleting an absent task is repeat safe.
func (DeleteTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes a task and classifies absence as completed deletion.
func (operation DeleteTaskOperation) Invoke(call sdkgo.Call, input DeleteTaskInput) sdkgo.MutationAttempt[DeletedTask] {
	if !validTaskID(input.TaskID) {
		return closeMutationBranch[DeletedTask]("deleteTask", DeleteTaskBranchDefect, sdkgo.FailureValidation, "taskId is invalid")
	}
	return invokeCloseWrite(operation.client, call, "deleteTask", http.MethodDelete, "/task/"+input.TaskID+"/", nil, DeleteTaskBranchDeleted, DeleteTaskBranchDeleted, func(body []byte) (DeletedTask, error) {
		var response map[string]json.RawMessage
		if err := json.Unmarshal(body, &response); err != nil || response == nil {
			return DeletedTask{}, errors.New("invalid deletion response")
		}
		return DeletedTask{TaskID: input.TaskID}, nil
	})
}

func validTaskID(taskID string) bool {
	return strings.HasPrefix(taskID, "task_") && safeKey.MatchString(taskID)
}
func validCloseUserID(userID string) bool {
	return strings.HasPrefix(userID, "user_") && safeKey.MatchString(userID)
}
func validOptionalTaskDate(date string) bool {
	if date == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", date)
	return err == nil
}

func decodeTask(body []byte) (Task, error) {
	var wire struct {
		ID         string `json:"id"`
		LeadID     string `json:"lead_id"`
		Text       string `json:"text"`
		IsComplete bool   `json:"is_complete"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return Task{}, err
	}
	if !validTaskID(wire.ID) || !validLeadID(wire.LeadID) {
		return Task{}, fmt.Errorf("incomplete task")
	}
	return Task{ID: wire.ID, LeadID: wire.LeadID, Text: wire.Text, IsComplete: wire.IsComplete}, nil
}

func decodeTaskPage(body []byte) (TaskPage, error) {
	var wire struct {
		Data    []json.RawMessage `json:"data"`
		HasMore bool              `json:"has_more"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TaskPage{}, err
	}
	if wire.Data == nil {
		return TaskPage{}, errors.New("task page is missing")
	}
	page := TaskPage{Tasks: make([]Task, 0, len(wire.Data)), HasMore: wire.HasMore}
	for _, row := range wire.Data {
		task, err := decodeTask(row)
		if err != nil {
			return TaskPage{}, err
		}
		page.Tasks = append(page.Tasks, task)
	}
	return page, nil
}
