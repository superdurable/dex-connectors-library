// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package copper

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateTaskInput supplies the new task's name.
type CreateTaskInput struct {
	// Name is the required display name.
	Name string `json:"name"`
}

// UpdateTaskInput changes the named fields of one task.
type UpdateTaskInput struct {
	// TaskID is a positive numeric ID.
	TaskID string `json:"taskId"`
	// Name replaces the display name.
	Name string `json:"name"`
	// Details replaces the notes; blank clears them.
	Details string `json:"details"`
}

// DeleteTaskInput identifies the task to remove permanently.
type DeleteTaskInput struct {
	// TaskID is a positive decimal Copper task ID.
	TaskID string `json:"taskId"`
}

// DeleteTaskOutput confirms a deleted task ID.
type DeleteTaskOutput struct {
	// ID is the deleted numeric ID.
	ID int64 `json:"id"`
	// Deleted is true when Copper confirms deletion.
	Deleted bool `json:"deleted"`
}

// CreateTaskOperation creates an task with a bounded send count.
type CreateTaskOperation struct{ client *Client }

// UpdateTaskOperation changes only the name and details.
type UpdateTaskOperation struct{ client *Client }

// DeleteTaskOperation removes an task permanently.
type DeleteTaskOperation struct{ client *Client }

// CreateTask binds the mutation to this client.
func (client *Client) CreateTask() CreateTaskOperation {
	return CreateTaskOperation{client}
}

// UpdateTask binds the mutation to this client.
func (client *Client) UpdateTask() UpdateTaskOperation {
	return UpdateTaskOperation{client}
}

// DeleteTask binds the mutation to this client.
func (client *Client) DeleteTask() DeleteTaskOperation {
	return DeleteTaskOperation{client}
}

// Definition returns the generated mutation contract.
func (CreateTaskOperation) Definition() sdkgo.MutationDefinition {
	return CreateTaskDefinition
}

// Definition returns the generated mutation contract.
func (UpdateTaskOperation) Definition() sdkgo.MutationDefinition {
	return UpdateTaskDefinition
}

// Definition returns the generated mutation contract.
func (DeleteTaskOperation) Definition() sdkgo.MutationDefinition {
	return DeleteTaskDefinition
}

// IdempotencyKey correlates bounded create attempts; Copper does not deduplicate this key.
func (CreateTaskOperation) IdempotencyKey(id sdkgo.CallID, _ CreateTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies attempts of an absolute field update.
func (UpdateTaskOperation) IdempotencyKey(id sdkgo.CallID, _ UpdateTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies attempts of a deletion.
func (DeleteTaskOperation) IdempotencyKey(id sdkgo.CallID, _ DeleteTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// Invoke replaces one task's name and notes, leaving other provider fields intact.
func (operation UpdateTaskOperation) Invoke(call sdkgo.Call, input UpdateTaskInput) sdkgo.MutationAttempt[TaskMetadata] {
	if !validTaskID(input.TaskID) || strings.TrimSpace(input.Name) == "" {
		return taskFailure[TaskMetadata]("updateTask", UpdateTaskBranchDefect, sdkgo.FailureValidation, "task ID and name are required")
	}
	body, _ := json.Marshal(struct {
		Name    string `json:"name"`
		Details string `json:"details"`
	}{input.Name, input.Details})
	response := operation.client.personRequest(call.Context, call, http.MethodPut, "/tasks/"+input.TaskID, body)
	if retry, failure := taskResponseFailure("updateTask", response); failure != nil {
		if retry {
			return sdkgo.NewMutationRetry[TaskMetadata](*failure, response.retryAfter)
		}
		branch := UpdateTaskBranchProviderRejected
		if response.status == http.StatusNotFound {
			branch = UpdateTaskBranchNotFound
		}
		if errors.Is(response.err, errCredential) {
			branch = UpdateTaskBranchDefect
		}
		return sdkgo.NewMutationBranch(branch, TaskMetadata{}, failure, sdkgo.Receipt{})
	}
	task, err := decodeTask(response.body)
	if response.status != http.StatusOK || err != nil {
		return sdkgo.NewMutationRetry[TaskMetadata](queryFailure("updateTask", sdkgo.FailureProtocol, "Copper update response is invalid"), 0)
	}
	return sdkgo.NewMutationBranch(UpdateTaskBranchUpdated, task, nil, sdkgo.Receipt{})
}

// Invoke permanently removes one task.
func (operation DeleteTaskOperation) Invoke(call sdkgo.Call, input DeleteTaskInput) sdkgo.MutationAttempt[DeleteTaskOutput] {
	if !validTaskID(input.TaskID) {
		return taskFailure[DeleteTaskOutput]("deleteTask", DeleteTaskBranchDefect, sdkgo.FailureValidation, "task ID must be positive")
	}
	response := operation.client.personRequest(call.Context, call, http.MethodDelete, "/tasks/"+input.TaskID, nil)
	if retry, failure := taskResponseFailure("deleteTask", response); failure != nil {
		if retry {
			return sdkgo.NewMutationRetry[DeleteTaskOutput](*failure, response.retryAfter)
		}
		branch := DeleteTaskBranchProviderRejected
		if response.status == http.StatusNotFound {
			branch = DeleteTaskBranchNotFound
		}
		if errors.Is(response.err, errCredential) {
			branch = DeleteTaskBranchDefect
		}
		return sdkgo.NewMutationBranch(branch, DeleteTaskOutput{}, failure, sdkgo.Receipt{})
	}
	var wire struct {
		ID      int64 `json:"id"`
		Deleted bool  `json:"is_deleted"`
	}
	if response.status != http.StatusOK || json.Unmarshal(response.body, &wire) != nil || wire.ID <= 0 || !wire.Deleted {
		return sdkgo.NewMutationRetry[DeleteTaskOutput](queryFailure("deleteTask", sdkgo.FailureProtocol, "Copper deletion response is invalid"), 0)
	}
	return sdkgo.NewMutationBranch(DeleteTaskBranchDeleted, DeleteTaskOutput{wire.ID, true}, nil, sdkgo.Receipt{})
}

// Invoke creates a task and reads back a saved provider ID before any resend.
func (operation CreateTaskOperation) Invoke(call sdkgo.Call, input CreateTaskInput) sdkgo.MutationAttempt[TaskMetadata] {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 {
		return taskFailure[TaskMetadata]("createTask", CreateTaskBranchDefect, sdkgo.FailureValidation, "name is required")
	}
	fence, err := singledispatch.NewFence(call, CreateTaskDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return taskFailure[TaskMetadata]("createTask", CreateTaskBranchDefect, sdkgo.FailureLocalDefect, "dispatch fence unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return taskUncertain("previous task creation cannot be confirmed")
	}
	if decision.Kind != singledispatch.SendAllowed {
		if decision.Marker.ProviderID != "" {
			response := operation.client.personRequest(call.Context, call, http.MethodGet, "/tasks/"+decision.Marker.ProviderID, nil)
			if response.err != nil || response.status == http.StatusTooManyRequests || response.status >= 500 {
				return sdkgo.NewMutationRetry[TaskMetadata](queryFailure("createTask", sdkgo.FailureAvailability, "Copper task read-back unavailable"), response.retryAfter)
			}
			if response.status != http.StatusOK {
				return taskUncertain("saved task ID cannot be confirmed")
			}
			task, err := decodeTask(response.body)
			if err != nil || strconv.FormatInt(task.ID, 10) != decision.Marker.ProviderID {
				return taskUncertain("saved task ID returned an invalid task")
			}
			return sdkgo.NewMutationBranch(CreateTaskBranchCreated, task, nil, sdkgo.Receipt{})
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return taskUncertain("previous task creation cannot be confirmed")
		}
	}
	body, err := json.Marshal(struct {
		Name string `json:"name"`
	}{Name: input.Name})
	if err != nil {
		return taskFailure[TaskMetadata]("createTask", CreateTaskBranchDefect, sdkgo.FailureLocalDefect, "task request could not be encoded")
	}
	var response personResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.personRequest(requestContext, call, http.MethodPost, "/tasks", body)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[TaskMetadata](queryFailure("createTask", sdkgo.FailureAvailability, "dispatch did not start"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[TaskMetadata](queryFailure("createTask", sdkgo.FailureAvailability, "retry checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[TaskMetadata](queryFailure("createTask", kind, "Copper did not create the task"), response.retryAfter)
	}
	if response.err != nil || response.status >= 500 {
		return sdkgo.NewMutationRetry[TaskMetadata](queryFailure("createTask", sdkgo.FailureTransport, "task creation outcome is unknown"), 0)
	}
	if response.status >= 400 {
		if decision.MaximumPriorSends > 0 {
			return taskUncertain("an earlier task creation may have succeeded")
		}
		return taskFailure[TaskMetadata]("createTask", CreateTaskBranchProviderRejected, sdkgo.FailureProviderRejection, "Copper rejected HTTP "+strconv.Itoa(response.status))
	}
	task, decodeErr := decodeTask(response.body)
	if response.status != http.StatusOK || decodeErr != nil {
		return sdkgo.NewMutationRetry[TaskMetadata](queryFailure("createTask", sdkgo.FailureProtocol, "task creation response is invalid"), 0)
	}
	if err := fence.RecordProviderID(strconv.FormatInt(task.ID, 10)); err != nil {
		return sdkgo.NewMutationRetry[TaskMetadata](queryFailure("createTask", sdkgo.FailureAvailability, "provider ID checkpoint unavailable"), 0)
	}
	return sdkgo.NewMutationBranch(CreateTaskBranchCreated, task, nil, sdkgo.Receipt{})
}

func validTaskID(id string) bool {
	value, err := strconv.ParseUint(id, 10, 63)
	return err == nil && value > 0 && strings.TrimSpace(id) == id
}

func taskResponseFailure(operation string, response personResponse) (bool, *sdkgo.Failure) {
	retry, failure := personResponseFailure(operation, response)
	if failure != nil {
		failure.Message = "Copper task request failed"
	}
	return retry, failure
}

func taskFailure[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	var zero T
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func taskUncertain(message string) sdkgo.MutationAttempt[TaskMetadata] {
	return sdkgo.NewMutationUncertain(TaskMetadata{}, queryFailure("createTask", sdkgo.FailureTransport, message), sdkgo.Receipt{})
}
