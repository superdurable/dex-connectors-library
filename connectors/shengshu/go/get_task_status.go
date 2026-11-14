// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shengshu

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetTaskStatusInput identifies an existing Vidu generation task.
type GetTaskStatusInput struct {
	// TaskID is the ID returned as task_id by a task-creation API.
	TaskID string `json:"taskId"`
}

// GetTaskStatusOutput omits provider payload and signed media URLs.
type GetTaskStatusOutput struct {
	// TaskID is the Vidu task identifier.
	TaskID string `json:"taskId"`
	// State is created, queueing, processing, success, or failed.
	State string `json:"state"`
	// ErrorCode is Vidu's task failure code, if any.
	ErrorCode string `json:"errorCode"`
	// Credits is the number of credits consumed by the task.
	Credits int64 `json:"credits"`
	// CreationIDs identifies produced media without exposing signed URLs.
	CreationIDs []string `json:"creationIds"`
}

// GetTaskStatusOperation reads one existing task without creating or modifying it.
type GetTaskStatusOperation struct{ client *Client }

// Definition returns the generated task-status contract.
func (GetTaskStatusOperation) Definition() sdkgo.QueryDefinition { return GetTaskStatusDefinition }

// Invoke reads one task and returns only durable-safe status fields.
func (operation GetTaskStatusOperation) Invoke(call sdkgo.Call, input GetTaskStatusInput) sdkgo.QueryAttempt[GetTaskStatusOutput] {
	if !isValidTaskID(input.TaskID) {
		return taskBranch(GetTaskStatusBranchDefect, sdkgo.FailureValidation, "taskId is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return taskBranch(GetTaskStatusBranchDefect, sdkgo.FailureAuthentication, "API key is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, taskEndpoint+input.TaskID+"/creations", nil)
	if err != nil {
		return taskBranch(GetTaskStatusBranchDefect, sdkgo.FailureValidation, "task-status request is invalid")
	}
	request.Header.Set("Authorization", "Token "+credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return taskRetry(sdkgo.FailureAvailability, "Vidu task-status request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return taskRetry(sdkgo.FailureRateLimit, "Vidu rate limited the task-status request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return taskRetry(sdkgo.FailureAvailability, "Vidu task status is temporarily unavailable", retryAfter)
	}
	if response.StatusCode == http.StatusNotFound {
		return taskBranch(GetTaskStatusBranchNotFound, sdkgo.FailureNotFound, "Vidu did not find the task")
	}
	if response.StatusCode != http.StatusOK {
		return classifyTaskStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return taskBranch(GetTaskStatusBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Vidu response exceeded the configured limit")
	}
	if err != nil {
		return taskRetry(sdkgo.FailureAvailability, "Vidu task-status response was interrupted", 0)
	}
	var wire struct {
		ID        string `json:"id"`
		State     string `json:"state"`
		ErrCode   string `json:"err_code"`
		Credits   *int64 `json:"credits"`
		Creations []struct {
			ID string `json:"id"`
		} `json:"creations"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.ID != input.TaskID || !isValidTaskState(wire.State) || wire.Credits == nil || *wire.Credits < 0 {
		return taskBranch(GetTaskStatusBranchInvalidResponse, sdkgo.FailureProtocol, "Vidu returned invalid task metadata")
	}
	output := GetTaskStatusOutput{TaskID: wire.ID, State: wire.State, ErrorCode: wire.ErrCode, Credits: *wire.Credits, CreationIDs: make([]string, 0, len(wire.Creations))}
	for _, creation := range wire.Creations {
		if creation.ID == "" {
			return taskBranch(GetTaskStatusBranchInvalidResponse, sdkgo.FailureProtocol, "Vidu returned invalid creation metadata")
		}
		output.CreationIDs = append(output.CreationIDs, creation.ID)
	}
	return sdkgo.NewQueryBranch(GetTaskStatusBranchFound, output, nil, sdkgo.Receipt{})
}
func isValidTaskID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_') {
			return false
		}
	}
	return true
}
func isValidTaskState(value string) bool {
	return value == "created" || value == "queueing" || value == "processing" || value == "success" || value == "failed"
}
func classifyTaskStatus(status int) sdkgo.QueryAttempt[GetTaskStatusOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return taskBranch(GetTaskStatusBranchProviderRejected, kind, "Vidu rejected the task-status request with HTTP "+strconv.Itoa(status))
}
func taskBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetTaskStatusOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "shengshu", Operation: "getTaskStatus", Message: message}
	return sdkgo.NewQueryBranch(branch, GetTaskStatusOutput{}, &failure, sdkgo.Receipt{})
}
func taskRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetTaskStatusOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "shengshu", Operation: "getTaskStatus", Message: message}
	return sdkgo.NewQueryRetry[GetTaskStatusOutput](failure, delay)
}
