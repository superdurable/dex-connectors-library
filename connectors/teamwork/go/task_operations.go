// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teamwork

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// ListTasksInput selects one bounded Teamwork task page.
type ListTasksInput struct {
	// Page is one-based; zero selects the first page.
	Page int `json:"page,omitempty"`
	// PageSize is from 1 through 500; zero selects 50.
	PageSize int `json:"pageSize,omitempty"`
}

// TaskPage contains task identity and state without private task details.
type TaskPage struct {
	// Tasks are the task summaries in this page.
	Tasks []TaskSummary `json:"tasks"`
	// Page is the requested one-based page.
	Page int `json:"page"`
}

// CreateTaskInput names a new task in an existing task list.
type CreateTaskInput struct {
	// TasklistID is the numeric Teamwork task list ID.
	TasklistID string `json:"tasklistId"`
	// Name is the new task title.
	Name string `json:"name"`
}

// UpdateTaskInput replaces a task's title.
type UpdateTaskInput struct {
	// TaskID is the numeric Teamwork task ID.
	TaskID string `json:"taskId"`
	// Name is the replacement title.
	Name string `json:"name"`
}

// DeleteTaskInput identifies a task to delete with its subtasks.
type DeleteTaskInput struct {
	// TaskID is the numeric Teamwork task ID.
	TaskID string `json:"taskId"`
}

// DeleteTaskOutput identifies the deleted or already absent task.
type DeleteTaskOutput struct {
	// ID is the requested task ID.
	ID string `json:"id"`
}

// ListTasksOperation reads one task page.
type ListTasksOperation struct{ client *Client }

// ListTasks binds the query to this client.
func (client *Client) ListTasks() ListTasksOperation { return ListTasksOperation{client} }

// Definition returns the generated query contract.
func (ListTasksOperation) Definition() sdkgo.QueryDefinition { return ListTasksDefinition }

// Invoke reads a task page with only identity and state.
func (operation ListTasksOperation) Invoke(call sdkgo.Call, input ListTasksInput) sdkgo.QueryAttempt[TaskPage] {
	if input.Page < 0 || input.PageSize < 0 || input.PageSize > 500 {
		return taskQueryBranch(ListTasksBranchDefect, TaskPage{}, "listTasks", sdkgo.FailureValidation, "invalid page selection")
	}
	if input.Page == 0 {
		input.Page = 1
	}
	if input.PageSize == 0 {
		input.PageSize = 50
	}
	query := url.Values{"page": {strconv.Itoa(input.Page)}, "pageSize": {strconv.Itoa(input.PageSize)}}
	response := operation.client.get(call, "/projects/api/v3/tasks.json?"+query.Encode())
	if retry, failure := taskResponseRetry("listTasks", response); retry {
		return sdkgo.NewQueryRetry[TaskPage](failure, response.retryAfter)
	}
	if response.err == errCredential {
		return taskQueryBranch(ListTasksBranchDefect, TaskPage{}, "listTasks", sdkgo.FailureAuthentication, "credentials unavailable")
	}
	if response.status != http.StatusOK {
		return taskQueryBranch(ListTasksBranchProviderRejected, TaskPage{}, "listTasks", sdkgo.FailureProviderRejection, "Teamwork rejected task listing")
	}
	if response.err != nil {
		return taskQueryBranch(ListTasksBranchInvalidResponse, TaskPage{}, "listTasks", sdkgo.FailureResponseTooLarge, "task page exceeded response limit")
	}
	var wire struct {
		Tasks []struct {
			ID     int64  `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"tasks"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Tasks == nil {
		return taskQueryBranch(ListTasksBranchInvalidResponse, TaskPage{}, "listTasks", sdkgo.FailureProtocol, "task page is invalid")
	}
	page := TaskPage{Tasks: make([]TaskSummary, 0, len(wire.Tasks)), Page: input.Page}
	for _, task := range wire.Tasks {
		if task.ID <= 0 || task.ID > 9007199254740991 || strings.TrimSpace(task.Name) == "" || strings.TrimSpace(task.Status) == "" {
			return taskQueryBranch(ListTasksBranchInvalidResponse, TaskPage{}, "listTasks", sdkgo.FailureProtocol, "task page contains an invalid task")
		}
		page.Tasks = append(page.Tasks, TaskSummary{ID: strconv.FormatInt(task.ID, 10), Name: task.Name, Status: task.Status})
	}
	return sdkgo.NewQueryBranch(ListTasksBranchListed, page, nil, sdkgo.Receipt{})
}

// CreateTaskOperation creates one task with a bounded resend cap.
type CreateTaskOperation struct{ client *Client }

// CreateTask binds the mutation to this client.
func (client *Client) CreateTask() CreateTaskOperation { return CreateTaskOperation{client} }

// Definition returns the generated mutation contract.
func (CreateTaskOperation) Definition() sdkgo.MutationDefinition { return CreateTaskDefinition }

// IdempotencyKey derives stable Dex call identity; Teamwork does not deduplicate it.
func (CreateTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one task per attempt and returns uncertainty when an unknown write reaches the cap.
func (operation CreateTaskOperation) Invoke(call sdkgo.Call, input CreateTaskInput) sdkgo.MutationAttempt[TaskSummary] {
	if !validTaskID(input.TasklistID) || strings.TrimSpace(input.Name) == "" {
		return taskMutationBranch(CreateTaskBranchDefect, TaskSummary{}, "createTask", sdkgo.FailureValidation, "task list ID and name are required")
	}
	body, err := json.Marshal(map[string]any{"task": map[string]string{"name": input.Name}})
	if err != nil {
		return taskMutationBranch(CreateTaskBranchDefect, TaskSummary{}, "createTask", sdkgo.FailureValidation, "task name cannot be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateTaskDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return taskMutationBranch(CreateTaskBranchDefect, TaskSummary{}, "createTask", sdkgo.FailureLocalDefect, "create checkpoint unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return taskMutationBranch(CreateTaskBranchDefect, TaskSummary{}, "createTask", sdkgo.FailureLocalDefect, "create checkpoint invalid")
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(TaskSummary{}, taskOperationFailure("createTask", sdkgo.FailureAvailability, "task create could not be confirmed"), sdkgo.Receipt{})
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.request(call, requestContext, http.MethodPost, "/projects/api/v3/tasklists/"+input.TasklistID+"/tasks.json", body)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[TaskSummary](taskOperationFailure("createTask", sdkgo.FailureAvailability, "task create outcome unknown"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[TaskSummary](taskOperationFailure("createTask", sdkgo.FailureAvailability, "retry checkpoint unavailable"), 0)
		}
	}
	if retry, failure := taskResponseRetry("createTask", response); retry {
		if response.status != http.StatusTooManyRequests && response.status != http.StatusServiceUnavailable && decision.MaximumPriorSends+1 >= call.MaxSends() {
			return sdkgo.NewMutationUncertain(TaskSummary{}, taskOperationFailure("createTask", sdkgo.FailureAvailability, "task create could not be confirmed"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[TaskSummary](failure, response.retryAfter)
	}
	if response.err == errCredential {
		return taskMutationBranch(CreateTaskBranchDefect, TaskSummary{}, "createTask", sdkgo.FailureAuthentication, "credentials unavailable")
	}
	if response.status >= 400 {
		return taskMutationBranch(CreateTaskBranchProviderRejected, TaskSummary{}, "createTask", sdkgo.FailureProviderRejection, "Teamwork rejected task create")
	}
	if response.status != http.StatusCreated {
		if decision.MaximumPriorSends+1 >= call.MaxSends() {
			return sdkgo.NewMutationUncertain(TaskSummary{}, taskOperationFailure("createTask", sdkgo.FailureAvailability, "task create response unconfirmed"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[TaskSummary](taskOperationFailure("createTask", sdkgo.FailureProtocol, "task create response unconfirmed"), 0)
	}
	task, err := decodeCreatedTask(response.body)
	if err != nil {
		if decision.MaximumPriorSends+1 >= call.MaxSends() {
			return sdkgo.NewMutationUncertain(TaskSummary{}, taskOperationFailure("createTask", sdkgo.FailureAvailability, "task create response invalid"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[TaskSummary](taskOperationFailure("createTask", sdkgo.FailureProtocol, "task create response invalid"), 0)
	}
	return sdkgo.NewMutationBranch(CreateTaskBranchCreated, task, nil, sdkgo.Receipt{})
}

// UpdateTaskOperation replaces a task title.
type UpdateTaskOperation struct{ client *Client }

// UpdateTask binds the mutation to this client.
func (client *Client) UpdateTask() UpdateTaskOperation { return UpdateTaskOperation{client} }

// Definition returns the generated mutation contract.
func (UpdateTaskOperation) Definition() sdkgo.MutationDefinition { return UpdateTaskDefinition }

// IdempotencyKey derives stable identity for absolute title replacement.
func (UpdateTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the task title without exposing other task details.
func (operation UpdateTaskOperation) Invoke(call sdkgo.Call, input UpdateTaskInput) sdkgo.MutationAttempt[TaskSummary] {
	if !validTaskID(input.TaskID) || strings.TrimSpace(input.Name) == "" {
		return taskMutationBranch(UpdateTaskBranchDefect, TaskSummary{}, "updateTask", sdkgo.FailureValidation, "task ID and name are required")
	}
	body, err := json.Marshal(map[string]any{"task": map[string]string{"name": input.Name}})
	if err != nil {
		return taskMutationBranch(UpdateTaskBranchDefect, TaskSummary{}, "updateTask", sdkgo.FailureValidation, "task name cannot be encoded")
	}
	response := operation.client.request(call, call.Context, http.MethodPatch, "/projects/api/v3/tasks/"+input.TaskID+".json", body)
	if retry, failure := taskResponseRetry("updateTask", response); retry {
		return sdkgo.NewMutationRetry[TaskSummary](failure, response.retryAfter)
	}
	if response.err == errCredential {
		return taskMutationBranch(UpdateTaskBranchDefect, TaskSummary{}, "updateTask", sdkgo.FailureAuthentication, "credentials unavailable")
	}
	if response.status == http.StatusNotFound {
		return taskMutationBranch(UpdateTaskBranchNotFound, TaskSummary{}, "updateTask", sdkgo.FailureNotFound, "task not found")
	}
	if response.status != http.StatusOK {
		return taskMutationBranch(UpdateTaskBranchProviderRejected, TaskSummary{}, "updateTask", sdkgo.FailureProviderRejection, "Teamwork rejected task update")
	}
	if response.err != nil {
		return taskMutationBranch(UpdateTaskBranchInvalidResponse, TaskSummary{}, "updateTask", sdkgo.FailureResponseTooLarge, "task response exceeded limit")
	}
	task, err := decodeTask(response.body, input.TaskID)
	if err != nil {
		return taskMutationBranch(UpdateTaskBranchInvalidResponse, TaskSummary{}, "updateTask", sdkgo.FailureProtocol, "task response invalid")
	}
	return sdkgo.NewMutationBranch(UpdateTaskBranchUpdated, task, nil, sdkgo.Receipt{})
}

// DeleteTaskOperation deletes a task and its subtasks.
type DeleteTaskOperation struct{ client *Client }

// DeleteTask binds the mutation to this client.
func (client *Client) DeleteTask() DeleteTaskOperation { return DeleteTaskOperation{client} }

// Definition returns the generated mutation contract.
func (DeleteTaskOperation) Definition() sdkgo.MutationDefinition { return DeleteTaskDefinition }

// IdempotencyKey derives stable identity for repeatable delete.
func (DeleteTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes a task and treats a definitive 404 as already absent.
func (operation DeleteTaskOperation) Invoke(call sdkgo.Call, input DeleteTaskInput) sdkgo.MutationAttempt[DeleteTaskOutput] {
	if !validTaskID(input.TaskID) {
		return taskMutationBranch(DeleteTaskBranchDefect, DeleteTaskOutput{}, "deleteTask", sdkgo.FailureValidation, "task ID is invalid")
	}
	response := operation.client.request(call, call.Context, http.MethodDelete, "/projects/api/v3/tasks/"+input.TaskID+".json", nil)
	if retry, failure := taskResponseRetry("deleteTask", response); retry {
		return sdkgo.NewMutationRetry[DeleteTaskOutput](failure, response.retryAfter)
	}
	if response.err == errCredential {
		return taskMutationBranch(DeleteTaskBranchDefect, DeleteTaskOutput{}, "deleteTask", sdkgo.FailureAuthentication, "credentials unavailable")
	}
	if response.status != http.StatusNoContent && response.status != http.StatusNotFound {
		return taskMutationBranch(DeleteTaskBranchProviderRejected, DeleteTaskOutput{}, "deleteTask", sdkgo.FailureProviderRejection, "Teamwork rejected task delete")
	}
	return sdkgo.NewMutationBranch(DeleteTaskBranchDeleted, DeleteTaskOutput{ID: input.TaskID}, nil, sdkgo.Receipt{})
}

func decodeCreatedTask(body []byte) (TaskSummary, error) {
	var wire struct {
		Task struct {
			ID     int64  `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"task"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TaskSummary{}, err
	}
	if wire.Task.ID <= 0 || wire.Task.ID > 9007199254740991 || strings.TrimSpace(wire.Task.Name) == "" || strings.TrimSpace(wire.Task.Status) == "" {
		return TaskSummary{}, errors.New("created task identity missing")
	}
	return TaskSummary{ID: strconv.FormatInt(wire.Task.ID, 10), Name: wire.Task.Name, Status: wire.Task.Status}, nil
}

func taskResponseRetry(operation string, response providerResponse) (bool, sdkgo.Failure) {
	if response.err == errRequest {
		return true, taskOperationFailure(operation, sdkgo.FailureTransport, "Teamwork request failed")
	}
	if response.status == http.StatusTooManyRequests {
		return true, taskOperationFailure(operation, sdkgo.FailureRateLimit, "Teamwork rate limited the request")
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return true, taskOperationFailure(operation, sdkgo.FailureAvailability, "Teamwork is unavailable")
	}
	if response.err != nil && !errors.Is(response.err, providerhttp.ErrBodyTooLarge) && response.err != errCredential {
		return true, taskOperationFailure(operation, sdkgo.FailureTransport, "Teamwork response interrupted")
	}
	return false, sdkgo.Failure{}
}

func taskOperationFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "teamwork", Operation: operation, Message: message}
}

func taskQueryBranch[Output any](branch sdkgo.BranchID, output Output, operation string, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[Output] {
	failure := taskOperationFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, output, &failure, sdkgo.Receipt{})
}

func taskMutationBranch[Output any](branch sdkgo.BranchID, output Output, operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[Output] {
	failure := taskOperationFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, output, &failure, sdkgo.Receipt{})
}
