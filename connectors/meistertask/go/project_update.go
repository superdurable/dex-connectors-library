// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meistertask

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UpdateProjectInput replaces selected project fields.
type UpdateProjectInput struct {
	// ProjectID is the positive project ID returned by MeisterTask.
	ProjectID string `json:"projectId"`
	// Name replaces the project name when provided; blank is invalid.
	Name *string `json:"name,omitempty"`
	// Notes replaces the description; blank clears it.
	Notes *string `json:"notes,omitempty"`
	// Status is 1 active, 4 trashed, or 5 archived.
	Status *int `json:"status,omitempty"`
}

// UpdateProjectOperation replaces selected project fields.
type UpdateProjectOperation struct{ client *Client }

// UpdateProject binds the Mutation to this client.
func (client *Client) UpdateProject() UpdateProjectOperation { return UpdateProjectOperation{client} }

// Definition returns the generated project Mutation contract.
func (UpdateProjectOperation) Definition() sdkgo.MutationDefinition { return UpdateProjectDefinition }

// IdempotencyKey supplies a stable call identity for the absolute update.
func (UpdateProjectOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one absolute project update.
func (operation UpdateProjectOperation) Invoke(call sdkgo.Call, input UpdateProjectInput) sdkgo.MutationAttempt[ProjectSummary] {
	if !validTaskID(input.ProjectID) || (input.Name == nil && input.Notes == nil && input.Status == nil) || (input.Name != nil && strings.TrimSpace(*input.Name) == "") || (input.Status != nil && *input.Status != 1 && *input.Status != 4 && *input.Status != 5) {
		return projectMutationBranch("updateProject", UpdateProjectBranchDefect, ProjectSummary{}, sdkgo.FailureValidation, "project ID or update fields are invalid")
	}
	payload, err := json.Marshal(struct {
		Name   *string `json:"name,omitempty"`
		Notes  *string `json:"notes,omitempty"`
		Status *int    `json:"status,omitempty"`
	}{input.Name, input.Notes, input.Status})
	if err != nil {
		return projectMutationBranch("updateProject", UpdateProjectBranchDefect, ProjectSummary{}, sdkgo.FailureLocalDefect, "project update could not be encoded")
	}
	response := operation.client.put(call, "/projects/"+input.ProjectID, payload)
	if errors.Is(response.err, errCredential) {
		return projectMutationBranch("updateProject", UpdateProjectBranchDefect, ProjectSummary{}, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[ProjectSummary](projectFailure("updateProject", sdkgo.FailureTransport, "MeisterTask request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[ProjectSummary](projectFailure("updateProject", sdkgo.FailureRateLimit, "MeisterTask rate limited the update"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[ProjectSummary](projectFailure("updateProject", sdkgo.FailureAvailability, "MeisterTask update outcome is not confirmed"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return projectMutationBranch("updateProject", UpdateProjectBranchNotFound, ProjectSummary{}, sdkgo.FailureNotFound, "MeisterTask did not expose the project")
	}
	if response.status != http.StatusOK {
		return projectMutationBranch("updateProject", UpdateProjectBranchProviderRejected, ProjectSummary{}, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return projectMutationBranch("updateProject", UpdateProjectBranchInvalidResponse, ProjectSummary{}, sdkgo.FailureResponseTooLarge, "MeisterTask response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[ProjectSummary](projectFailure("updateProject", sdkgo.FailureTransport, "MeisterTask response was interrupted"), 0)
	}
	project, err := decodeProject(response.body, input.ProjectID)
	if err != nil {
		return projectMutationBranch("updateProject", UpdateProjectBranchInvalidResponse, ProjectSummary{}, sdkgo.FailureProtocol, "MeisterTask returned invalid project JSON")
	}
	return sdkgo.NewMutationBranch(UpdateProjectBranchUpdated, project, nil, sdkgo.Receipt{})
}

func projectMutationBranch[Output any](operation string, branch sdkgo.BranchID, value Output, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[Output] {
	failure := projectFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}
