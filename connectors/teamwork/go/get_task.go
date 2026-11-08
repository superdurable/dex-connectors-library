// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teamwork

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

// GetTaskInput selects a task by its non-secret Teamwork ID.
type GetTaskInput struct {
	// TaskID is the ID shown in Teamwork Tasks or returned by the API.
	TaskID string `json:"taskId"`
}

// TaskSummary excludes descriptions and assignee details.
type TaskSummary struct {
	// ID is the Teamwork task ID.
	ID string `json:"id"`
	// Name is the task title.
	Name string `json:"name"`
	// Status is the task workflow state such as pending.
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
		return taskBranch(GetTaskBranchDefect, sdkgo.FailureValidation, "task ID must be a positive decimal integer")
	}
	response := operation.client.get(call, "/projects/api/v3/tasks/"+url.PathEscape(input.TaskID)+".json")
	if errors.Is(response.err, errCredential) {
		return taskBranch(GetTaskBranchDefect, sdkgo.FailureAuthentication, "Teamwork credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[TaskSummary](taskFailure(sdkgo.FailureTransport, "Teamwork request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[TaskSummary](taskFailure(sdkgo.FailureRateLimit, "Teamwork rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[TaskSummary](taskFailure(sdkgo.FailureAvailability, "Teamwork is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return taskBranch(GetTaskBranchNotFound, sdkgo.FailureNotFound, "Teamwork did not expose the task")
	}
	if response.status != http.StatusOK {
		return taskBranch(GetTaskBranchProviderRejected, sdkgo.FailureProviderRejection, "Teamwork rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return taskBranch(GetTaskBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Teamwork response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[TaskSummary](taskFailure(sdkgo.FailureTransport, "Teamwork response was interrupted"), 0)
	}
	taskSummary, err := decodeTask(response.body, input.TaskID)
	if err != nil {
		return taskBranch(GetTaskBranchInvalidResponse, sdkgo.FailureProtocol, "Teamwork returned invalid task JSON")
	}
	return sdkgo.NewQueryBranch(GetTaskBranchFound, taskSummary, nil, sdkgo.Receipt{})
}

func validTaskID(taskID string) bool {
	if len(taskID) == 0 || len(taskID) > 19 || taskID[0] == '0' {
		return false
	}
	for _, character := range taskID {
		if character >= '0' && character <= '9' {
			continue
		}
		return false
	}
	parsedID, err := strconv.ParseInt(taskID, 10, 64)
	return err == nil && parsedID <= 9007199254740991
}

func decodeTask(body []byte, requestedTaskID string) (TaskSummary, error) {
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
	if strconv.FormatInt(wire.Task.ID, 10) != requestedTaskID || strings.TrimSpace(wire.Task.Name) == "" || strings.TrimSpace(wire.Task.Status) == "" {
		return TaskSummary{}, fmt.Errorf("task identity, name, or status missing")
	}
	return TaskSummary{ID: requestedTaskID, Name: wire.Task.Name, Status: wire.Task.Status}, nil
}

func taskFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "teamwork", Operation: "getTask", Message: message}
}

func taskBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[TaskSummary] {
	failure := taskFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, TaskSummary{}, &failure, sdkgo.Receipt{})
}
