// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meistertask

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UpdateTaskInput sets selected task fields to absolute values.
type UpdateTaskInput struct {
	// TaskID is the positive task ID returned by MeisterTask.
	TaskID string `json:"taskId"`
	// Name replaces the task name when provided; an empty name is invalid.
	Name *string `json:"name,omitempty"`
	// Notes replaces task notes when provided; an empty string clears them.
	Notes *string `json:"notes,omitempty"`
	// Status sets 1 open, 2 completed, 8 trashed, or 18 completed and archived.
	Status *int `json:"status,omitempty"`
	// AssignedToID replaces the assignee with a positive person ID.
	AssignedToID *int64 `json:"assignedToId,omitempty"`
	// SectionID moves the task to a positive destination section ID.
	SectionID *int64 `json:"sectionId,omitempty"`
	// Due replaces the due date with an RFC 3339 timestamp.
	Due *string `json:"due,omitempty"`
}

// UpdateTaskOperation replaces selected fields of one task.
type UpdateTaskOperation struct{ client *Client }

// UpdateTask binds the Mutation to this client.
func (client *Client) UpdateTask() UpdateTaskOperation { return UpdateTaskOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (UpdateTaskOperation) Definition() sdkgo.MutationDefinition { return UpdateTaskDefinition }

// IdempotencyKey supplies a stable call identity; absolute updates repeat safely without provider deduplication.
func (UpdateTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one absolute update and classifies definite and unknown outcomes.
func (operation UpdateTaskOperation) Invoke(call sdkgo.Call, input UpdateTaskInput) sdkgo.MutationAttempt[TaskSummary] {
	if !validTaskID(input.TaskID) || (input.Name == nil && input.Notes == nil && input.Status == nil && input.AssignedToID == nil && input.SectionID == nil && input.Due == nil) ||
		(input.Name != nil && strings.TrimSpace(*input.Name) == "") ||
		(input.Status != nil && *input.Status != 1 && *input.Status != 2 && *input.Status != 8 && *input.Status != 18) ||
		(input.AssignedToID != nil && !validTaskID(strconv.FormatInt(*input.AssignedToID, 10))) ||
		(input.SectionID != nil && !validTaskID(strconv.FormatInt(*input.SectionID, 10))) ||
		(input.Due != nil && !validDueDate(*input.Due)) {
		return updateTaskBranch(UpdateTaskBranchDefect, sdkgo.FailureValidation, "task ID or update fields are invalid")
	}
	payload, err := json.Marshal(struct {
		Name         *string `json:"name,omitempty"`
		Notes        *string `json:"notes,omitempty"`
		Status       *int    `json:"status,omitempty"`
		AssignedToID *int64  `json:"assigned_to_id,omitempty"`
		SectionID    *int64  `json:"section_id,omitempty"`
		Due          *string `json:"due,omitempty"`
	}{Name: input.Name, Notes: input.Notes, Status: input.Status, AssignedToID: input.AssignedToID, SectionID: input.SectionID, Due: input.Due})
	if err != nil {
		return updateTaskBranch(UpdateTaskBranchDefect, sdkgo.FailureLocalDefect, "task update could not be encoded")
	}
	response := operation.client.put(call, "/tasks/"+input.TaskID, payload)
	if errors.Is(response.err, errCredential) {
		return updateTaskBranch(UpdateTaskBranchDefect, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[TaskSummary](updateTaskFailure(sdkgo.FailureTransport, "MeisterTask request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[TaskSummary](updateTaskFailure(sdkgo.FailureRateLimit, "MeisterTask rate limited the update"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[TaskSummary](updateTaskFailure(sdkgo.FailureAvailability, "MeisterTask update outcome is not confirmed"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return updateTaskBranch(UpdateTaskBranchNotFound, sdkgo.FailureNotFound, "MeisterTask did not expose the task")
	}
	if response.status != http.StatusOK {
		return updateTaskBranch(UpdateTaskBranchProviderRejected, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return updateTaskBranch(UpdateTaskBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "MeisterTask response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[TaskSummary](updateTaskFailure(sdkgo.FailureTransport, "MeisterTask response was interrupted"), 0)
	}
	taskSummary, err := decodeTask(response.body, input.TaskID)
	if err != nil {
		return updateTaskBranch(UpdateTaskBranchInvalidResponse, sdkgo.FailureProtocol, "MeisterTask returned invalid task JSON")
	}
	return sdkgo.NewMutationBranch(UpdateTaskBranchUpdated, taskSummary, nil, sdkgo.Receipt{})
}

func validDueDate(value string) bool {
	_, err := time.Parse(time.RFC3339, value)
	return err == nil
}

func (client *Client) put(call sdkgo.Call, path string, payload []byte) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	apiToken := credentials.APIToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(apiToken) {
		return providerResponse{err: errCredential}
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPut, client.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiToken)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: body, status: response.StatusCode, retryAfter: retryAfter, err: err}
}

func updateTaskFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "meistertask", Operation: "updateTask", Message: message}
}

func updateTaskBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[TaskSummary] {
	failure := updateTaskFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, TaskSummary{}, &failure, sdkgo.Receipt{})
}
