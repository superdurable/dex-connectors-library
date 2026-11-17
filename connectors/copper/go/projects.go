// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package copper

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateProjectInput supplies the new project's name.
type CreateProjectInput struct {
	// Name is the required display name.
	Name string `json:"name"`
}

// UpdateProjectInput changes the named fields of one project.
type UpdateProjectInput struct {
	// ProjectID is a positive numeric ID.
	ProjectID string `json:"projectId"`
	// Name replaces the display name.
	Name string `json:"name"`
	// Details replaces the notes; blank clears them.
	Details string `json:"details"`
}

// DeleteProjectInput identifies the project to remove permanently.
type DeleteProjectInput struct {
	// ProjectID is a positive decimal Copper project ID.
	ProjectID string `json:"projectId"`
}

// DeleteProjectOutput confirms a deleted project ID.
type DeleteProjectOutput struct {
	// ID is the deleted numeric ID.
	ID int64 `json:"id"`
	// Deleted is true when Copper confirms deletion.
	Deleted bool `json:"deleted"`
}

// CreateProjectOperation creates an project with a bounded send count.
type CreateProjectOperation struct{ client *Client }

// UpdateProjectOperation changes only the name and details.
type UpdateProjectOperation struct{ client *Client }

// DeleteProjectOperation removes an project permanently.
type DeleteProjectOperation struct{ client *Client }

// CreateProject binds the mutation to this client.
func (client *Client) CreateProject() CreateProjectOperation {
	return CreateProjectOperation{client}
}

// UpdateProject binds the mutation to this client.
func (client *Client) UpdateProject() UpdateProjectOperation {
	return UpdateProjectOperation{client}
}

// DeleteProject binds the mutation to this client.
func (client *Client) DeleteProject() DeleteProjectOperation {
	return DeleteProjectOperation{client}
}

// Definition returns the generated mutation contract.
func (CreateProjectOperation) Definition() sdkgo.MutationDefinition {
	return CreateProjectDefinition
}

// Definition returns the generated mutation contract.
func (UpdateProjectOperation) Definition() sdkgo.MutationDefinition {
	return UpdateProjectDefinition
}

// Definition returns the generated mutation contract.
func (DeleteProjectOperation) Definition() sdkgo.MutationDefinition {
	return DeleteProjectDefinition
}

// IdempotencyKey correlates bounded create attempts; Copper does not deduplicate this key.
func (CreateProjectOperation) IdempotencyKey(id sdkgo.CallID, _ CreateProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies attempts of an absolute field update.
func (UpdateProjectOperation) IdempotencyKey(id sdkgo.CallID, _ UpdateProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies attempts of a deletion.
func (DeleteProjectOperation) IdempotencyKey(id sdkgo.CallID, _ DeleteProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// Invoke replaces one project's name and notes, leaving other provider fields intact.
func (operation UpdateProjectOperation) Invoke(call sdkgo.Call, input UpdateProjectInput) sdkgo.MutationAttempt[ProjectMetadata] {
	if !validProjectID(input.ProjectID) || strings.TrimSpace(input.Name) == "" {
		return projectFailure[ProjectMetadata]("updateProject", UpdateProjectBranchDefect, sdkgo.FailureValidation, "project ID and name are required")
	}
	body, _ := json.Marshal(struct {
		Name    string `json:"name"`
		Details string `json:"details"`
	}{input.Name, input.Details})
	response := operation.client.personRequest(call.Context, call, http.MethodPut, "/projects/"+input.ProjectID, body)
	if retry, failure := projectResponseFailure("updateProject", response); failure != nil {
		if retry {
			return sdkgo.NewMutationRetry[ProjectMetadata](*failure, response.retryAfter)
		}
		branch := UpdateProjectBranchProviderRejected
		if response.status == http.StatusNotFound {
			branch = UpdateProjectBranchNotFound
		}
		if errors.Is(response.err, errCredential) {
			branch = UpdateProjectBranchDefect
		}
		return sdkgo.NewMutationBranch(branch, ProjectMetadata{}, failure, sdkgo.Receipt{})
	}
	project, err := decodeProject(response.body)
	if response.status != http.StatusOK || err != nil {
		return sdkgo.NewMutationRetry[ProjectMetadata](queryFailure("updateProject", sdkgo.FailureProtocol, "Copper update response is invalid"), 0)
	}
	return sdkgo.NewMutationBranch(UpdateProjectBranchUpdated, project, nil, sdkgo.Receipt{})
}

// Invoke permanently removes one project.
func (operation DeleteProjectOperation) Invoke(call sdkgo.Call, input DeleteProjectInput) sdkgo.MutationAttempt[DeleteProjectOutput] {
	if !validProjectID(input.ProjectID) {
		return projectFailure[DeleteProjectOutput]("deleteProject", DeleteProjectBranchDefect, sdkgo.FailureValidation, "project ID must be positive")
	}
	response := operation.client.personRequest(call.Context, call, http.MethodDelete, "/projects/"+input.ProjectID, nil)
	if retry, failure := projectResponseFailure("deleteProject", response); failure != nil {
		if retry {
			return sdkgo.NewMutationRetry[DeleteProjectOutput](*failure, response.retryAfter)
		}
		branch := DeleteProjectBranchProviderRejected
		if response.status == http.StatusNotFound {
			branch = DeleteProjectBranchNotFound
		}
		if errors.Is(response.err, errCredential) {
			branch = DeleteProjectBranchDefect
		}
		return sdkgo.NewMutationBranch(branch, DeleteProjectOutput{}, failure, sdkgo.Receipt{})
	}
	var wire struct {
		ID      int64 `json:"id"`
		Deleted bool  `json:"is_deleted"`
	}
	if response.status != http.StatusOK || json.Unmarshal(response.body, &wire) != nil || wire.ID <= 0 || !wire.Deleted {
		return sdkgo.NewMutationRetry[DeleteProjectOutput](queryFailure("deleteProject", sdkgo.FailureProtocol, "Copper deletion response is invalid"), 0)
	}
	return sdkgo.NewMutationBranch(DeleteProjectBranchDeleted, DeleteProjectOutput{wire.ID, true}, nil, sdkgo.Receipt{})
}

// Invoke creates an project and searches its name before any resend.
func (operation CreateProjectOperation) Invoke(call sdkgo.Call, input CreateProjectInput) sdkgo.MutationAttempt[ProjectMetadata] {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 {
		return projectFailure[ProjectMetadata]("createProject", CreateProjectBranchDefect, sdkgo.FailureValidation, "name is required")
	}
	fence, err := singledispatch.NewFence(call, CreateProjectDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return projectFailure[ProjectMetadata]("createProject", CreateProjectBranchDefect, sdkgo.FailureLocalDefect, "dispatch fence unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return projectUncertain("previous project creation cannot be confirmed")
	}
	if decision.Kind != singledispatch.SendAllowed {
		found, isKnown, retry := operation.client.findProjectByName(call, input.Name)
		if retry {
			return sdkgo.NewMutationRetry[ProjectMetadata](queryFailure("createProject", sdkgo.FailureAvailability, "Copper project search unavailable"), 0)
		}
		if found != nil {
			if decision.Marker.ProviderID == strconv.FormatInt(found.ID, 10) {
				return sdkgo.NewMutationBranch(CreateProjectBranchCreated, *found, nil, sdkgo.Receipt{})
			}
			return projectUncertain("a matching project cannot prove this creation")
		}
		if decision.Kind == singledispatch.ReadBackRequired || !isKnown {
			return projectUncertain("previous project creation cannot be confirmed")
		}
	}
	body, err := json.Marshal(struct {
		Name string `json:"name"`
	}{Name: input.Name})
	if err != nil {
		return projectFailure[ProjectMetadata]("createProject", CreateProjectBranchDefect, sdkgo.FailureLocalDefect, "project request could not be encoded")
	}
	var response personResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.personRequest(requestContext, call, http.MethodPost, "/projects", body)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[ProjectMetadata](queryFailure("createProject", sdkgo.FailureAvailability, "dispatch did not start"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[ProjectMetadata](queryFailure("createProject", sdkgo.FailureAvailability, "retry checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[ProjectMetadata](queryFailure("createProject", kind, "Copper did not create the project"), response.retryAfter)
	}
	if response.err != nil || response.status >= 500 {
		return sdkgo.NewMutationRetry[ProjectMetadata](queryFailure("createProject", sdkgo.FailureTransport, "project creation outcome is unknown"), 0)
	}
	if response.status >= 400 {
		if decision.MaximumPriorSends > 0 {
			return projectUncertain("an earlier project creation may have succeeded")
		}
		return projectFailure[ProjectMetadata]("createProject", CreateProjectBranchProviderRejected, sdkgo.FailureProviderRejection, "Copper rejected HTTP "+strconv.Itoa(response.status))
	}
	project, decodeErr := decodeProject(response.body)
	if response.status != http.StatusOK || decodeErr != nil {
		return sdkgo.NewMutationRetry[ProjectMetadata](queryFailure("createProject", sdkgo.FailureProtocol, "project creation response is invalid"), 0)
	}
	if err := fence.RecordProviderID(strconv.FormatInt(project.ID, 10)); err != nil {
		return sdkgo.NewMutationRetry[ProjectMetadata](queryFailure("createProject", sdkgo.FailureAvailability, "provider ID checkpoint unavailable"), 0)
	}
	return sdkgo.NewMutationBranch(CreateProjectBranchCreated, project, nil, sdkgo.Receipt{})
}

func (client *Client) findProjectByName(call sdkgo.Call, name string) (*ProjectMetadata, bool, bool) {
	body, err := json.Marshal(struct {
		Name string `json:"name"`
	}{name})
	if err != nil {
		return nil, false, true
	}
	response := client.personRequest(call.Context, call, http.MethodPost, "/projects/search", body)
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status >= 500 {
		return nil, false, true
	}
	if response.status != http.StatusOK {
		return nil, false, false
	}
	var records []json.RawMessage
	if json.Unmarshal(response.body, &records) != nil || records == nil {
		return nil, false, true
	}
	for _, record := range records {
		project, err := decodeProject(record)
		if err != nil {
			return nil, false, true
		}
		if project.Name == name {
			return &project, true, false
		}
	}
	return nil, true, false
}

func validProjectID(id string) bool {
	value, err := strconv.ParseUint(id, 10, 63)
	return err == nil && value > 0 && strings.TrimSpace(id) == id
}

func projectResponseFailure(operation string, response personResponse) (bool, *sdkgo.Failure) {
	retry, failure := personResponseFailure(operation, response)
	if failure != nil {
		failure.Message = "Copper project request failed"
	}
	return retry, failure
}

func projectFailure[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	var zero T
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func projectUncertain(message string) sdkgo.MutationAttempt[ProjectMetadata] {
	return sdkgo.NewMutationUncertain(ProjectMetadata{}, queryFailure("createProject", sdkgo.FailureTransport, message), sdkgo.Receipt{})
}
