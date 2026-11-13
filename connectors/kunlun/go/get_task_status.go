// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kunlun

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetTaskStatusInput identifies an existing Mureka song-generation task.
type GetTaskStatusInput struct {
	// TaskID is the id returned by a song-generation request.
	TaskID string `json:"taskId"`
}

// GetTaskStatusOutput contains task metadata without generated lyrics or media URLs.
type GetTaskStatusOutput struct {
	// TaskID is Mureka's task identifier.
	TaskID string `json:"taskId"`
	// Status is the observed song-generation state.
	Status string `json:"status"`
	// Model identifies the model used for generation.
	Model string `json:"model"`
}

// GetTaskStatusOperation reads one existing Mureka song task.
type GetTaskStatusOperation struct{ client *Client }

// Definition returns the generated song-task status contract.
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
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, taskEndpoint+input.TaskID, nil)
	if err != nil {
		return taskBranch(GetTaskStatusBranchDefect, sdkgo.FailureValidation, "song-task request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return taskRetry(sdkgo.FailureAvailability, "Mureka song-task request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		body, readErr := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
		if readErr == nil {
			var providerError struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(body, &providerError) == nil && strings.Contains(strings.ToLower(providerError.Error.Message), "exceeded your current quota") {
				return taskBranch(GetTaskStatusBranchProviderRejected, sdkgo.FailureProviderRejection, "Mureka account quota is exhausted")
			}
		}
		return taskRetry(sdkgo.FailureRateLimit, "Mureka rate limited the song-task request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return taskRetry(sdkgo.FailureAvailability, "Mureka song task is temporarily unavailable", retryAfter)
	}
	if response.StatusCode == http.StatusNotFound {
		return taskBranch(GetTaskStatusBranchNotFound, sdkgo.FailureNotFound, "Mureka did not find the song task")
	}
	if response.StatusCode != http.StatusOK {
		return classifyTaskStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return taskBranch(GetTaskStatusBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Mureka response exceeded the configured limit")
	}
	if err != nil {
		return taskRetry(sdkgo.FailureAvailability, "Mureka song-task response was interrupted", 0)
	}
	var wire struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Model  string `json:"model"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.ID != input.TaskID || !isValidTaskStatus(wire.Status) {
		return taskBranch(GetTaskStatusBranchInvalidResponse, sdkgo.FailureProtocol, "Mureka returned invalid song-task metadata")
	}
	return sdkgo.NewQueryBranch(GetTaskStatusBranchFound, GetTaskStatusOutput{TaskID: wire.ID, Status: wire.Status, Model: wire.Model}, nil, sdkgo.Receipt{})
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
func isValidTaskStatus(value string) bool {
	switch value {
	case "preparing", "queued", "running", "streaming", "reviewing", "succeeded", "failed", "timeouted", "cancelled":
		return true
	default:
		return false
	}
}
func classifyTaskStatus(status int) sdkgo.QueryAttempt[GetTaskStatusOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return taskBranch(GetTaskStatusBranchProviderRejected, kind, "Mureka rejected the song-task request with HTTP "+strconv.Itoa(status))
}
func taskBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetTaskStatusOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "kunlun", Operation: "getTaskStatus", Message: message}
	return sdkgo.NewQueryBranch(branch, GetTaskStatusOutput{}, &failure, sdkgo.Receipt{})
}
func taskRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetTaskStatusOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "kunlun", Operation: "getTaskStatus", Message: message}
	return sdkgo.NewQueryRetry[GetTaskStatusOutput](failure, delay)
}
