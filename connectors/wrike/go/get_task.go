// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wrike

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetTaskInput selects a task by its non-secret Wrike ID.
type GetTaskInput struct {
	// TaskID is the ID shown in Wrike Tasks or returned by the API.
	TaskID string `json:"taskId"`
}

// TaskStatus excludes descriptions, custom fields, and assignees.
type TaskStatus struct {
	// ID is the Wrike task ID.
	ID string `json:"id"`
	// Title is the task's display title.
	Title string `json:"title"`
	// Status is the task state such as Active.
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
func (operation GetTaskOperation) Invoke(call sdkgo.Call, input GetTaskInput) sdkgo.QueryAttempt[TaskStatus] {
	if !validTaskID(input.TaskID) {
		return taskBranch(GetTaskBranchDefect, sdkgo.FailureValidation, "task ID must be 1–256 URL-safe characters")
	}
	response := operation.client.get(call, "/tasks/"+url.PathEscape(input.TaskID))
	if errors.Is(response.err, errCredential) {
		return taskBranch(GetTaskBranchDefect, sdkgo.FailureAuthentication, "Wrike credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[TaskStatus](taskFailure(sdkgo.FailureTransport, "Wrike request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[TaskStatus](taskFailure(sdkgo.FailureRateLimit, "Wrike rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[TaskStatus](taskFailure(sdkgo.FailureAvailability, "Wrike is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return taskBranch(GetTaskBranchNotFound, sdkgo.FailureNotFound, "Wrike did not expose the task")
	}
	if response.status != http.StatusOK {
		return taskBranch(GetTaskBranchProviderRejected, sdkgo.FailureProviderRejection, "Wrike rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return taskBranch(GetTaskBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Wrike response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[TaskStatus](taskFailure(sdkgo.FailureTransport, "Wrike response was interrupted"), 0)
	}
	taskStatus, err := decodeTask(response.body, input.TaskID)
	if err != nil {
		return taskBranch(GetTaskBranchInvalidResponse, sdkgo.FailureProtocol, "Wrike returned invalid task JSON")
	}
	return sdkgo.NewQueryBranch(GetTaskBranchFound, taskStatus, nil, sdkgo.Receipt{})
}

func validTaskID(taskID string) bool {
	if len(taskID) == 0 || len(taskID) > 256 {
		return false
	}
	for _, character := range taskID {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' ||
			character == ':' || character == '.' || character == '=' {
			continue
		}
		return false
	}
	return true
}

func decodeTask(body []byte, requestedTaskID string) (TaskStatus, error) {
	var wire struct {
		Kind string `json:"kind"`
		Data []struct {
			ID     string `json:"id"`
			Title  string `json:"title"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TaskStatus{}, err
	}
	if wire.Kind != "tasks" || len(wire.Data) != 1 || wire.Data[0].ID != requestedTaskID || strings.TrimSpace(wire.Data[0].Title) == "" || strings.TrimSpace(wire.Data[0].Status) == "" {
		return TaskStatus{}, fmt.Errorf("task identity, title, or status missing")
	}
	return TaskStatus{ID: wire.Data[0].ID, Title: wire.Data[0].Title, Status: wire.Data[0].Status}, nil
}

func taskFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "wrike", Operation: "getTask", Message: message}
}

func taskBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[TaskStatus] {
	failure := taskFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, TaskStatus{}, &failure, sdkgo.Receipt{})
}
