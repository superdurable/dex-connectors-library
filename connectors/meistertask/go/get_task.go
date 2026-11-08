// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meistertask

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetTaskInput selects a task by its non-secret MeisterTask ID.
type GetTaskInput struct {
	// TaskID is the ID shown in MeisterTask Tasks or returned by the API.
	TaskID string `json:"taskId"`
}

// TaskSummary contains the task's ID, name, and numeric status code.
type TaskSummary struct {
	// ID is the MeisterTask task ID.
	ID string `json:"id"`
	// Name is the task name.
	Name string `json:"name"`
	// Status is MeisterTask's numeric status code represented as decimal text.
	Status string `json:"status"`
}

// GetTaskOperation reads one task's identity and state.
type GetTaskOperation struct{ client *Client }

// GetTask binds the Query to this client.
func (client *Client) GetTask() GetTaskOperation {
	return GetTaskOperation{client}
}

// Definition returns the manifest-generated Query task.
func (GetTaskOperation) Definition() sdkgo.QueryDefinition { return GetTaskDefinition }

// Invoke reads one task and classifies retryable and definitive outcomes.
func (operation GetTaskOperation) Invoke(call sdkgo.Call, input GetTaskInput) sdkgo.QueryAttempt[TaskSummary] {
	if !validTaskID(input.TaskID) {
		return taskBranch(GetTaskBranchDefect, sdkgo.FailureValidation, "task ID must be a positive 32-bit decimal integer")
	}
	response := operation.client.get(call, "/tasks/"+url.PathEscape(input.TaskID))
	if errors.Is(response.err, errCredential) {
		return taskBranch(GetTaskBranchDefect, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[TaskSummary](taskFailure(sdkgo.FailureTransport, "MeisterTask request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[TaskSummary](taskFailure(sdkgo.FailureRateLimit, "MeisterTask rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[TaskSummary](taskFailure(sdkgo.FailureAvailability, "MeisterTask is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return taskBranch(GetTaskBranchNotFound, sdkgo.FailureNotFound, "MeisterTask did not expose the task")
	}
	if response.status != http.StatusOK {
		return taskBranch(GetTaskBranchProviderRejected, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return taskBranch(GetTaskBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "MeisterTask response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[TaskSummary](taskFailure(sdkgo.FailureTransport, "MeisterTask response was interrupted"), 0)
	}
	taskSummary, err := decodeTask(response.body, input.TaskID)
	if err != nil {
		return taskBranch(GetTaskBranchInvalidResponse, sdkgo.FailureProtocol, "MeisterTask returned invalid task JSON")
	}
	return sdkgo.NewQueryBranch(GetTaskBranchFound, taskSummary, nil, sdkgo.Receipt{})
}

func validTaskID(taskID string) bool {
	if len(taskID) == 0 || len(taskID) > 10 || taskID[0] == '0' {
		return false
	}
	for _, character := range taskID {
		if character >= '0' && character <= '9' {
			continue
		}
		return false
	}
	parsedID, err := strconv.ParseInt(taskID, 10, 32)
	return err == nil && parsedID > 0
}

func decodeTask(body []byte, requestedTaskID string) (TaskSummary, error) {
	var wire struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Status *int64 `json:"status"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TaskSummary{}, err
	}
	if strconv.FormatInt(wire.ID, 10) != requestedTaskID || wire.Name == "" || wire.Status == nil || *wire.Status < 0 {
		return TaskSummary{}, fmt.Errorf("task identity, name, or status missing")
	}
	return TaskSummary{ID: requestedTaskID, Name: wire.Name, Status: strconv.FormatInt(*wire.Status, 10)}, nil
}

func taskFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "meistertask", Operation: "getTask", Message: message}
}

func taskBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[TaskSummary] {
	failure := taskFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, TaskSummary{}, &failure, sdkgo.Receipt{})
}
