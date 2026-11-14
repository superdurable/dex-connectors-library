// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dedao

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var taskIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// GetNoteTaskStatusInput identifies an existing asynchronous note-save task.
type GetNoteTaskStatusInput struct {
	// TaskID is data.tasks[0].task_id returned by a prior note-save call.
	TaskID string `json:"taskID"`
}

// GetNoteTaskStatusOutput contains status and the resulting note ID without note content.
type GetNoteTaskStatusOutput struct {
	// TaskID is the requested asynchronous task identifier.
	TaskID string `json:"taskID"`
	// Status is pending, processing, success, or failed.
	Status string `json:"status"`
	// NoteID is set when the provider returns one for a successful task.
	NoteID string `json:"noteID"`
}

// GetNoteTaskStatusOperation queries one task's current status.
type GetNoteTaskStatusOperation struct{ client *Client }

// Definition returns the generated task-status Query contract.
func (GetNoteTaskStatusOperation) Definition() sdkgo.QueryDefinition {
	return GetNoteTaskStatusDefinition
}

// Invoke reads task status through the documented OpenAPI endpoint.
func (operation GetNoteTaskStatusOperation) Invoke(call sdkgo.Call, input GetNoteTaskStatusInput) sdkgo.QueryAttempt[GetNoteTaskStatusOutput] {
	if !taskIDPattern.MatchString(input.TaskID) {
		return statusBranch(GetNoteTaskStatusBranchDefect, sdkgo.FailureValidation, "Get Note task ID is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.APIKey.Reveal()) || !isValidCredential(credentials.ClientID.Reveal()) {
		return statusBranch(GetNoteTaskStatusBranchDefect, sdkgo.FailureAuthentication, "Get Note credentials are unavailable or invalid")
	}
	body, err := json.Marshal(struct {
		TaskID string `json:"task_id"`
	}{input.TaskID})
	if err != nil {
		return statusBranch(GetNoteTaskStatusBranchDefect, sdkgo.FailureValidation, "Get Note task request is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, taskStatusEndpoint, bytes.NewReader(body))
	if err != nil {
		return statusBranch(GetNoteTaskStatusBranchDefect, sdkgo.FailureValidation, "Get Note task request is invalid")
	}
	request.Header.Set("Authorization", credentials.APIKey.Reveal())
	request.Header.Set("X-Client-ID", credentials.ClientID.Reveal())
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return statusRetry(sdkgo.FailureAvailability, "Get Note task request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return statusRetry(sdkgo.FailureRateLimit, "Get Note rate limited task request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return statusRetry(sdkgo.FailureAvailability, "Get Note API is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return statusBranch(GetNoteTaskStatusBranchProviderRejected, kind, "Get Note rejected task request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	content, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return statusBranch(GetNoteTaskStatusBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Get Note task response exceeded the configured limit")
	}
	if err != nil {
		return statusRetry(sdkgo.FailureAvailability, "Get Note task response was interrupted", 0)
	}
	var wire struct {
		Success *bool `json:"success"`
		Data    *struct {
			Status string `json:"status"`
			NoteID string `json:"note_id"`
		} `json:"data"`
	}
	if json.Unmarshal(content, &wire) != nil || wire.Success == nil {
		return statusBranch(GetNoteTaskStatusBranchInvalidResponse, sdkgo.FailureProtocol, "Get Note returned invalid task data")
	}
	if !*wire.Success {
		return statusBranch(GetNoteTaskStatusBranchProviderRejected, sdkgo.FailureProviderRejection, "Get Note rejected the task request")
	}
	if wire.Data == nil {
		return statusBranch(GetNoteTaskStatusBranchInvalidResponse, sdkgo.FailureProtocol, "Get Note omitted task data")
	}
	switch wire.Data.Status {
	case "pending", "processing", "success", "failed":
	default:
		return statusBranch(GetNoteTaskStatusBranchInvalidResponse, sdkgo.FailureProtocol, "Get Note returned unknown task status")
	}
	if len(wire.Data.NoteID) > 128 {
		return statusBranch(GetNoteTaskStatusBranchInvalidResponse, sdkgo.FailureProtocol, "Get Note returned invalid note ID")
	}
	return sdkgo.NewQueryBranch(GetNoteTaskStatusBranchFound, GetNoteTaskStatusOutput{TaskID: input.TaskID, Status: wire.Data.Status, NoteID: wire.Data.NoteID}, nil, sdkgo.Receipt{})
}

func statusBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetNoteTaskStatusOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "dedao", Operation: "getNoteTaskStatus", Message: message}
	return sdkgo.NewQueryBranch(branch, GetNoteTaskStatusOutput{}, &failure, sdkgo.Receipt{})
}
func statusRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetNoteTaskStatusOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "dedao", Operation: "getNoteTaskStatus", Message: message}
	return sdkgo.NewQueryRetry[GetNoteTaskStatusOutput](failure, delay)
}
