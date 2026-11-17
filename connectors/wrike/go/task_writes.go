// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wrike

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateTaskOperation creates one task with bounded resend.
type CreateTaskOperation struct{ client *Client }

// CreateTask binds the Mutation to this client.
func (client *Client) CreateTask() CreateTaskOperation { return CreateTaskOperation{client: client} }

// Definition returns the manifest-generated Mutation contract.
func (CreateTaskOperation) Definition() sdkgo.MutationDefinition { return CreateTaskDefinition }

// IdempotencyKey records the call identity; Wrike does not deduplicate it.
func (CreateTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a task in the selected folder.
func (operation CreateTaskOperation) Invoke(call sdkgo.Call, input CreateTaskInput) sdkgo.MutationAttempt[TaskStatus] {
	if !validTaskID(input.FolderID) || strings.TrimSpace(input.Title) == "" {
		return taskWriteBranch("createTask", CreateTaskBranchDefect, sdkgo.FailureValidation, "folder ID and title are required")
	}
	parameters := url.Values{"title": {input.Title}}
	if input.Description != "" {
		parameters.Set("description", input.Description)
	}
	return operation.client.writeTask(call, "createTask", http.MethodPost, "/folders/"+url.PathEscape(input.FolderID)+"/tasks?"+parameters.Encode(), true,
		CreateTaskBranchCreated, "", CreateTaskBranchProviderRejected, CreateTaskBranchInvalidResponse, CreateTaskBranchDefect)
}

// UpdateTaskOperation replaces a task title or status.
type UpdateTaskOperation struct{ client *Client }

// UpdateTask binds the Mutation to this client.
func (client *Client) UpdateTask() UpdateTaskOperation { return UpdateTaskOperation{client: client} }

// Definition returns the manifest-generated Mutation contract.
func (UpdateTaskOperation) Definition() sdkgo.MutationDefinition { return UpdateTaskDefinition }

// IdempotencyKey records the stable call identity.
func (UpdateTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces selected task fields.
func (operation UpdateTaskOperation) Invoke(call sdkgo.Call, input UpdateTaskInput) sdkgo.MutationAttempt[TaskStatus] {
	if !validTaskID(input.TaskID) || input.Title == "" && input.Status == "" || input.Status != "" && !validTaskStatus(input.Status) {
		return taskWriteBranch("updateTask", UpdateTaskBranchDefect, sdkgo.FailureValidation, "task ID, title, or status is invalid")
	}
	parameters := url.Values{}
	if input.Title != "" {
		parameters.Set("title", input.Title)
	}
	if input.Status != "" {
		parameters.Set("status", input.Status)
	}
	return operation.client.writeTask(call, "updateTask", http.MethodPut, "/tasks/"+url.PathEscape(input.TaskID)+"?"+parameters.Encode(), false,
		UpdateTaskBranchUpdated, UpdateTaskBranchNotFound, UpdateTaskBranchProviderRejected, UpdateTaskBranchInvalidResponse, UpdateTaskBranchDefect)
}

// DeleteTaskOperation moves a task to the Recycle Bin.
type DeleteTaskOperation struct{ client *Client }

// DeleteTask binds the Mutation to this client.
func (client *Client) DeleteTask() DeleteTaskOperation { return DeleteTaskOperation{client: client} }

// Definition returns the manifest-generated Mutation contract.
func (DeleteTaskOperation) Definition() sdkgo.MutationDefinition { return DeleteTaskDefinition }

// IdempotencyKey records the stable call identity.
func (DeleteTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke moves one task to the Recycle Bin.
func (operation DeleteTaskOperation) Invoke(call sdkgo.Call, input DeleteTaskInput) sdkgo.MutationAttempt[TaskStatus] {
	if !validTaskID(input.TaskID) {
		return taskWriteBranch("deleteTask", DeleteTaskBranchDefect, sdkgo.FailureValidation, "task ID is invalid")
	}
	return operation.client.writeTask(call, "deleteTask", http.MethodDelete, "/tasks/"+url.PathEscape(input.TaskID), false,
		DeleteTaskBranchDeleted, DeleteTaskBranchNotFound, DeleteTaskBranchProviderRejected, DeleteTaskBranchInvalidResponse, DeleteTaskBranchDefect)
}

func validTaskStatus(status string) bool {
	return status == "Active" || status == "Deferred" || status == "Completed" || status == "Cancelled"
}

func (client *Client) writeTask(call sdkgo.Call, operation, method, path string, isLimited bool,
	successBranch, notFoundBranch, rejectedBranch, invalidBranch, defectBranch sdkgo.BranchID) sdkgo.MutationAttempt[TaskStatus] {
	var fence *singledispatch.Fence
	if isLimited {
		var err error
		fence, err = singledispatch.NewFence(call, CreateTaskDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
		if err != nil {
			return taskWriteBranch(operation, defectBranch, sdkgo.FailureLocalDefect, "dispatch fence is unavailable")
		}
		decision := fence.Decide()
		if decision.Marker.ProviderID != "" {
			return client.readBackTask(call, operation, decision.Marker.ProviderID, successBranch)
		}
		if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
			return taskWriteUncertain(operation, sdkgo.FailureTransport, "task create outcome cannot be confirmed after the send cap")
		}
	}
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		if fence != nil && fence.Decide().Kind == singledispatch.SendAllowed {
			if recordErr := fence.RecordNotSent(); recordErr != nil {
				return taskWriteRetry(operation, sdkgo.FailureAvailability, "credential checkpoint is unavailable", 0)
			}
		}
		return taskWriteBranch(operation, defectBranch, sdkgo.FailureAuthentication, "Wrike credentials are unavailable")
	}
	apiToken := credentials.APIToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(apiToken) {
		return taskWriteBranch(operation, defectBranch, sdkgo.FailureAuthentication, "Wrike token is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, method, client.baseURL+path, nil)
	if err != nil {
		return taskWriteBranch(operation, defectBranch, sdkgo.FailureLocalDefect, "Wrike request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiToken)
	var response *http.Response
	if fence != nil {
		if err := singledispatch.PrepareHTTPWrite(request); err != nil {
			return taskWriteBranch(operation, defectBranch, sdkgo.FailureLocalDefect, "Wrike request is invalid")
		}
		err = fence.Dispatch(func(requestContext context.Context) error {
			response, err = client.httpClient.Do(request.WithContext(requestContext))
			return err
		})
	} else {
		response, err = client.httpClient.Do(request)
	}
	if err != nil {
		if fence != nil && (errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime)) {
			return taskWriteRetry(operation, sdkgo.FailureAvailability, "provider request was not sent", 0)
		}
		if fence != nil && fence.Decide().MaximumPriorSends+1 >= call.MaxSends() {
			return taskWriteUncertain(operation, sdkgo.FailureTransport, "task create send cap was reached after a transport failure")
		}
		return taskWriteRetry(operation, sdkgo.FailureTransport, "Wrike write failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if fence != nil && fence.RecordCanResend() != nil {
			return taskWriteRetry(operation, sdkgo.FailureAvailability, "retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return taskWriteRetry(operation, kind, "Wrike asked to retry the write", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		if fence != nil && fence.Decide().MaximumPriorSends+1 >= call.MaxSends() {
			return taskWriteUncertain(operation, sdkgo.FailureAvailability, "task create send cap was reached after an unknown response")
		}
		return taskWriteRetry(operation, sdkgo.FailureAvailability, "Wrike write outcome is unknown", 0)
	}
	if response.StatusCode == http.StatusNotFound && notFoundBranch != "" {
		return taskWriteBranch(operation, notFoundBranch, sdkgo.FailureNotFound, "Wrike task was not found")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return taskWriteBranch(operation, rejectedBranch, sdkgo.FailureProviderRejection, "Wrike rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return taskWriteBranch(operation, invalidBranch, sdkgo.FailureResponseTooLarge, "Wrike response exceeded the configured limit")
	}
	if strings.Contains(string(body), apiToken) {
		return taskWriteBranch(operation, defectBranch, sdkgo.FailureAuthentication, "Wrike response contained a credential")
	}
	task, err := decodeSingleTask(body)
	if err != nil {
		return taskWriteBranch(operation, invalidBranch, sdkgo.FailureProtocol, "Wrike returned invalid task JSON")
	}
	if fence != nil && fence.RecordProviderID(task.ID) != nil {
		return taskWriteRetry(operation, sdkgo.FailureAvailability, "task identity checkpoint is unavailable", 0)
	}
	return sdkgo.NewMutationBranch(successBranch, task, nil, sdkgo.Receipt{})
}

func (client *Client) readBackTask(call sdkgo.Call, operation, taskID string, successBranch sdkgo.BranchID) sdkgo.MutationAttempt[TaskStatus] {
	response := client.get(call, "/tasks/"+url.PathEscape(taskID))
	if response.err != nil || response.status >= 500 || response.status == http.StatusTooManyRequests {
		return taskWriteRetry(operation, sdkgo.FailureAvailability, "created task read-back is unavailable", 0)
	}
	if response.status != http.StatusOK {
		return taskWriteUncertain(operation, sdkgo.FailureProtocol, "created task could not be confirmed")
	}
	task, err := decodeSingleTask(response.body)
	if err != nil || task.ID != taskID {
		return taskWriteUncertain(operation, sdkgo.FailureProtocol, "created task could not be confirmed")
	}
	return sdkgo.NewMutationBranch(successBranch, task, nil, sdkgo.Receipt{})
}

func decodeSingleTask(body []byte) (TaskStatus, error) {
	var wire struct {
		Kind string       `json:"kind"`
		Data []TaskStatus `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TaskStatus{}, err
	}
	if wire.Kind != "tasks" || len(wire.Data) != 1 || !validTaskID(wire.Data[0].ID) || strings.TrimSpace(wire.Data[0].Title) == "" || wire.Data[0].Status == "" {
		return TaskStatus{}, errors.New("task response is incomplete")
	}
	return wire.Data[0], nil
}

func taskWriteFailure(operation string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: "wrike", Operation: operation, Message: message}
}
func taskWriteBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[TaskStatus] {
	return sdkgo.NewMutationBranch(branch, TaskStatus{}, taskWriteFailure(operation, kind, message), sdkgo.Receipt{})
}
func taskWriteRetry(operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[TaskStatus] {
	return sdkgo.NewMutationRetry[TaskStatus](*taskWriteFailure(operation, kind, message), delay)
}
func taskWriteUncertain(operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[TaskStatus] {
	return sdkgo.NewMutationUncertain(TaskStatus{}, *taskWriteFailure(operation, kind, message), sdkgo.Receipt{})
}
