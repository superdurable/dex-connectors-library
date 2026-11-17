// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meistertask

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ProjectLabelSummary identifies one label defined in a project.
type ProjectLabelSummary struct {
	// ID is the positive label ID.
	ID string `json:"id"`
	// ProjectID is the parent project ID.
	ProjectID string `json:"projectId"`
	// Name is the label's visible name.
	Name string `json:"name"`
}

// ProjectLabels contains labels defined in one project.
type ProjectLabels struct {
	// Labels are returned in provider order.
	Labels []ProjectLabelSummary `json:"labels"`
	// ProjectID is the requested parent project ID.
	ProjectID string `json:"projectId"`
}

// TaskLabelSummary identifies one label attachment.
type TaskLabelSummary struct {
	// ID is the positive task-label attachment ID.
	ID string `json:"id"`
	// TaskID is the parent task ID.
	TaskID string `json:"taskId"`
	// LabelID is the attached project label ID.
	LabelID string `json:"labelId"`
}

// TaskLabels contains one task's label attachments.
type TaskLabels struct {
	// Labels are returned in provider order.
	Labels []TaskLabelSummary `json:"labels"`
	// TaskID is the requested parent task ID.
	TaskID string `json:"taskId"`
}

// ListProjectLabelsInput identifies a project.
type ListProjectLabelsInput struct {
	// ProjectID is the positive parent project ID.
	ProjectID string `json:"projectId"`
}

// ListTaskLabelsInput identifies a task.
type ListTaskLabelsInput struct {
	// TaskID is the positive parent task ID.
	TaskID string `json:"taskId"`
}

// ListProjectLabelsOperation reads labels defined in one project.
type ListProjectLabelsOperation struct{ client *Client }

// ListProjectLabels binds the Query to this client.
func (client *Client) ListProjectLabels() ListProjectLabelsOperation {
	return ListProjectLabelsOperation{client}
}

// Definition returns the generated project-label Query contract.
func (ListProjectLabelsOperation) Definition() sdkgo.QueryDefinition {
	return ListProjectLabelsDefinition
}

// Invoke lists label identities and names in one project.
func (operation ListProjectLabelsOperation) Invoke(call sdkgo.Call, input ListProjectLabelsInput) sdkgo.QueryAttempt[ProjectLabels] {
	if !validTaskID(input.ProjectID) {
		return labelQueryBranch("listProjectLabels", ListProjectLabelsBranchDefect, ProjectLabels{}, sdkgo.FailureValidation, "project ID is invalid")
	}
	response := operation.client.get(call, "/projects/"+input.ProjectID+"/labels")
	if decision, handled := classifyLabelQueryResponse("listProjectLabels", response, ProjectLabels{}, ListProjectLabelsBranchProviderRejected, ListProjectLabelsBranchInvalidResponse, ListProjectLabelsBranchDefect); handled {
		return decision
	}
	var wire []json.RawMessage
	if err := json.Unmarshal(response.body, &wire); err != nil || wire == nil {
		return labelQueryBranch("listProjectLabels", ListProjectLabelsBranchInvalidResponse, ProjectLabels{}, sdkgo.FailureProtocol, "MeisterTask returned invalid labels JSON")
	}
	labels := make([]ProjectLabelSummary, 0, len(wire))
	for _, raw := range wire {
		var value struct {
			ID        int64  `json:"id"`
			ProjectID int64  `json:"project_id"`
			Name      string `json:"name"`
		}
		if json.Unmarshal(raw, &value) != nil || !validTaskID(strconv.FormatInt(value.ID, 10)) || strconv.FormatInt(value.ProjectID, 10) != input.ProjectID || value.Name == "" {
			return labelQueryBranch("listProjectLabels", ListProjectLabelsBranchInvalidResponse, ProjectLabels{}, sdkgo.FailureProtocol, "MeisterTask returned incomplete label metadata")
		}
		labels = append(labels, ProjectLabelSummary{ID: strconv.FormatInt(value.ID, 10), ProjectID: input.ProjectID, Name: value.Name})
	}
	return sdkgo.NewQueryBranch(ListProjectLabelsBranchListed, ProjectLabels{Labels: labels, ProjectID: input.ProjectID}, nil, sdkgo.Receipt{})
}

// ListTaskLabelsOperation reads label attachments on one task.
type ListTaskLabelsOperation struct{ client *Client }

// ListTaskLabels binds the Query to this client.
func (client *Client) ListTaskLabels() ListTaskLabelsOperation {
	return ListTaskLabelsOperation{client}
}

// Definition returns the generated task-label Query contract.
func (ListTaskLabelsOperation) Definition() sdkgo.QueryDefinition { return ListTaskLabelsDefinition }

// Invoke lists attachment IDs and their label IDs.
func (operation ListTaskLabelsOperation) Invoke(call sdkgo.Call, input ListTaskLabelsInput) sdkgo.QueryAttempt[TaskLabels] {
	if !validTaskID(input.TaskID) {
		return labelQueryBranch("listTaskLabels", ListTaskLabelsBranchDefect, TaskLabels{}, sdkgo.FailureValidation, "task ID is invalid")
	}
	response := operation.client.get(call, "/tasks/"+input.TaskID+"/task_labels")
	if decision, handled := classifyLabelQueryResponse("listTaskLabels", response, TaskLabels{}, ListTaskLabelsBranchProviderRejected, ListTaskLabelsBranchInvalidResponse, ListTaskLabelsBranchDefect); handled {
		return decision
	}
	var wire []json.RawMessage
	if err := json.Unmarshal(response.body, &wire); err != nil || wire == nil {
		return labelQueryBranch("listTaskLabels", ListTaskLabelsBranchInvalidResponse, TaskLabels{}, sdkgo.FailureProtocol, "MeisterTask returned invalid task-label JSON")
	}
	labels := make([]TaskLabelSummary, 0, len(wire))
	for _, raw := range wire {
		label, err := decodeTaskLabel(raw, "")
		if err != nil || label.TaskID != input.TaskID {
			return labelQueryBranch("listTaskLabels", ListTaskLabelsBranchInvalidResponse, TaskLabels{}, sdkgo.FailureProtocol, "MeisterTask returned incomplete task-label metadata")
		}
		labels = append(labels, label)
	}
	return sdkgo.NewQueryBranch(ListTaskLabelsBranchListed, TaskLabels{Labels: labels, TaskID: input.TaskID}, nil, sdkgo.Receipt{})
}

func decodeTaskLabel(body []byte, requestedID string) (TaskLabelSummary, error) {
	var wire struct {
		ID      int64 `json:"id"`
		TaskID  int64 `json:"task_id"`
		LabelID int64 `json:"label_id"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TaskLabelSummary{}, err
	}
	id, taskID, labelID := strconv.FormatInt(wire.ID, 10), strconv.FormatInt(wire.TaskID, 10), strconv.FormatInt(wire.LabelID, 10)
	if !validTaskID(id) || !validTaskID(taskID) || !validTaskID(labelID) || (requestedID != "" && requestedID != id) {
		return TaskLabelSummary{}, errors.New("task-label identity missing")
	}
	return TaskLabelSummary{ID: id, TaskID: taskID, LabelID: labelID}, nil
}

func classifyLabelQueryResponse[Output any](operation string, response providerResponse, empty Output, rejected, invalid, defect sdkgo.BranchID) (sdkgo.QueryAttempt[Output], bool) {
	if errors.Is(response.err, errCredential) {
		return labelQueryBranch(operation, defect, empty, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid"), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[Output](labelFailure(operation, sdkgo.FailureTransport, "MeisterTask request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[Output](labelFailure(operation, sdkgo.FailureRateLimit, "MeisterTask rate limited the request"), response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[Output](labelFailure(operation, sdkgo.FailureAvailability, "MeisterTask is temporarily unavailable"), response.retryAfter), true
	}
	if response.status != http.StatusOK {
		return labelQueryBranch(operation, rejected, empty, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return labelQueryBranch(operation, invalid, empty, sdkgo.FailureResponseTooLarge, "MeisterTask response exceeded the configured limit"), true
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[Output](labelFailure(operation, sdkgo.FailureTransport, "MeisterTask response was interrupted"), 0), true
	}
	return sdkgo.QueryAttempt[Output]{}, false
}

func labelFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "meistertask", Operation: operation, Message: message}
}
func labelQueryBranch[Output any](operation string, branch sdkgo.BranchID, value Output, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[Output] {
	failure := labelFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}
