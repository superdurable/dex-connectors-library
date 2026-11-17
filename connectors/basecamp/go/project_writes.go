// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package basecamp

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateProjectInput supplies the account, project name, and description.
type CreateProjectInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// Name is the non-blank project name.
	Name string `json:"name"`
	// Description is optional project context.
	Description string `json:"description,omitempty"`
}

// UpdateProjectInput replaces the selected project's name and description.
type UpdateProjectInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// ProjectID comes from listProjects.
	ProjectID string `json:"projectId"`
	// Name is the non-blank replacement name.
	Name string `json:"name"`
	// Description replaces the existing description; blank clears it.
	Description string `json:"description"`
}

// ProjectActionResult identifies the project and its requested status.
type ProjectActionResult struct {
	// ProjectID is the selected project.
	ProjectID string `json:"projectId"`
	// Status is archived, active, or trashed.
	Status string `json:"status"`
}

// CreateProjectOperation creates one project with a bounded send count.
type CreateProjectOperation struct{ client *Client }

// CreateProject binds the project creation Mutation.
func (client *Client) CreateProject() CreateProjectOperation { return CreateProjectOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (CreateProjectOperation) Definition() sdkgo.MutationDefinition { return CreateProjectDefinition }

// IdempotencyKey records the Dex call identity; Basecamp does not deduplicate it.
func (CreateProjectOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a project, repeating ambiguous sends only within the configured cap.
func (operation CreateProjectOperation) Invoke(call sdkgo.Call, input CreateProjectInput) sdkgo.MutationAttempt[Project] {
	if !validResourceID(input.AccountID) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 512 || len(input.Description) > 10000 {
		return projectMutationBranch[Project]("createProject", CreateProjectBranchDefect, sdkgo.FailureValidation, "project input is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateProjectDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return projectMutationBranch[Project]("createProject", CreateProjectBranchDefect, sdkgo.FailureLocalDefect, "project dispatch checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return projectMutationUncertain[Project]("createProject", "project creation may have completed without a stable lookup key")
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.accountMutation(call, requestContext, http.MethodPost, input.AccountID, "/projects.json", struct {
			Name        string `json:"name"`
			Description string `json:"description,omitempty"`
		}{input.Name, input.Description})
		return nil
	})
	if err != nil {
		return projectMutationRetry[Project]("createProject", sdkgo.FailureAvailability, "project dispatch was deferred", 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return projectMutationRetry[Project]("createProject", sdkgo.FailureAvailability, "project resend checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return projectMutationRetry[Project]("createProject", kind, "Basecamp temporarily rejected project creation", response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return projectMutationUncertain[Project]("createProject", "an earlier project creation may have completed")
		}
		return projectMutationBranch[Project]("createProject", CreateProjectBranchDefect, sdkgo.FailureAuthentication, "Basecamp credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return projectMutationRetry[Project]("createProject", sdkgo.FailureTransport, "project creation outcome is unknown", response.retryAfter)
	}
	if response.status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return projectMutationUncertain[Project]("createProject", "an earlier project creation may have completed")
		}
		return projectMutationBranch[Project]("createProject", CreateProjectBranchProviderRejected, sdkgo.FailureProviderRejection, "Basecamp rejected project creation")
	}
	project, err := decodeProject(response.body, "")
	if err != nil || project.Name != input.Name {
		return projectMutationRetry[Project]("createProject", sdkgo.FailureProtocol, "Basecamp returned invalid created project JSON", 0)
	}
	return sdkgo.NewMutationBranch(CreateProjectBranchCreated, project, nil, sdkgo.Receipt{})
}

// UpdateProjectOperation replaces one project's name and description.
type UpdateProjectOperation struct{ client *Client }

// UpdateProject binds the project update Mutation.
func (client *Client) UpdateProject() UpdateProjectOperation { return UpdateProjectOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (UpdateProjectOperation) Definition() sdkgo.MutationDefinition { return UpdateProjectDefinition }

// IdempotencyKey records the stable Dex call identity.
func (UpdateProjectOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends an absolute project name and description replacement.
func (operation UpdateProjectOperation) Invoke(call sdkgo.Call, input UpdateProjectInput) sdkgo.MutationAttempt[Project] {
	if !validResourceID(input.AccountID) || !validResourceID(input.ProjectID) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 512 || len(input.Description) > 10000 {
		return projectMutationBranch[Project]("updateProject", UpdateProjectBranchDefect, sdkgo.FailureValidation, "project input is invalid")
	}
	response := operation.client.accountMutation(call, call.Context, http.MethodPut, input.AccountID, "/projects/"+input.ProjectID+".json", struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}{input.Name, input.Description})
	if attempt, done := projectMutationResponse[Project](response, "updateProject", http.StatusOK, UpdateProjectBranchNotFound, UpdateProjectBranchProviderRejected, UpdateProjectBranchDefect, UpdateProjectBranchInvalidResponse); done {
		return attempt
	}
	project, err := decodeProject(response.body, input.ProjectID)
	if err != nil || project.Name != input.Name {
		return projectMutationBranch[Project]("updateProject", UpdateProjectBranchInvalidResponse, sdkgo.FailureProtocol, "Basecamp returned invalid updated project JSON")
	}
	return sdkgo.NewMutationBranch(UpdateProjectBranchUpdated, project, nil, sdkgo.Receipt{})
}

// ArchiveProjectOperation marks a project archived.
type ArchiveProjectOperation struct{ client *Client }

// ArchiveProject binds the project archive Mutation.
func (client *Client) ArchiveProject() ArchiveProjectOperation {
	return ArchiveProjectOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (ArchiveProjectOperation) Definition() sdkgo.MutationDefinition { return ArchiveProjectDefinition }

// IdempotencyKey records the stable Dex call identity.
func (ArchiveProjectOperation) IdempotencyKey(callID sdkgo.CallID, _ GetProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke marks the selected project archived.
func (operation ArchiveProjectOperation) Invoke(call sdkgo.Call, input GetProjectInput) sdkgo.MutationAttempt[ProjectActionResult] {
	return operation.client.setProjectStatus(call, input, http.MethodPut, "/status/archived.json", "archiveProject", "archived", ArchiveProjectBranchArchived, ArchiveProjectBranchNotFound, ArchiveProjectBranchProviderRejected, ArchiveProjectBranchDefect)
}

// UnarchiveProjectOperation restores a project to active status.
type UnarchiveProjectOperation struct{ client *Client }

// UnarchiveProject binds the project restore Mutation.
func (client *Client) UnarchiveProject() UnarchiveProjectOperation {
	return UnarchiveProjectOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (UnarchiveProjectOperation) Definition() sdkgo.MutationDefinition {
	return UnarchiveProjectDefinition
}

// IdempotencyKey records the stable Dex call identity.
func (UnarchiveProjectOperation) IdempotencyKey(callID sdkgo.CallID, _ GetProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke restores the selected project to active status.
func (operation UnarchiveProjectOperation) Invoke(call sdkgo.Call, input GetProjectInput) sdkgo.MutationAttempt[ProjectActionResult] {
	return operation.client.setProjectStatus(call, input, http.MethodPut, "/status/active.json", "unarchiveProject", "active", UnarchiveProjectBranchActive, UnarchiveProjectBranchNotFound, UnarchiveProjectBranchProviderRejected, UnarchiveProjectBranchDefect)
}

// TrashProjectOperation moves a project to the trash.
type TrashProjectOperation struct{ client *Client }

// TrashProject binds the project trash Mutation.
func (client *Client) TrashProject() TrashProjectOperation { return TrashProjectOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (TrashProjectOperation) Definition() sdkgo.MutationDefinition { return TrashProjectDefinition }

// IdempotencyKey records the stable Dex call identity.
func (TrashProjectOperation) IdempotencyKey(callID sdkgo.CallID, _ GetProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke trashes the selected project.
func (operation TrashProjectOperation) Invoke(call sdkgo.Call, input GetProjectInput) sdkgo.MutationAttempt[ProjectActionResult] {
	return operation.client.setProjectStatus(call, input, http.MethodDelete, ".json", "trashProject", "trashed", TrashProjectBranchTrashed, TrashProjectBranchNotFound, TrashProjectBranchProviderRejected, TrashProjectBranchDefect)
}

func (client *Client) setProjectStatus(call sdkgo.Call, input GetProjectInput, method, suffix, operation, status string, success, notFound, rejected, defect sdkgo.BranchID) sdkgo.MutationAttempt[ProjectActionResult] {
	if !validResourceID(input.AccountID) || !validResourceID(input.ProjectID) {
		return projectMutationBranch[ProjectActionResult](operation, defect, sdkgo.FailureValidation, "project IDs are invalid")
	}
	response := client.accountMutation(call, call.Context, method, input.AccountID, "/projects/"+input.ProjectID+suffix, nil)
	if attempt, done := projectMutationResponse[ProjectActionResult](response, operation, http.StatusNoContent, notFound, rejected, defect, rejected); done {
		return attempt
	}
	return sdkgo.NewMutationBranch(success, ProjectActionResult{ProjectID: input.ProjectID, Status: status}, nil, sdkgo.Receipt{})
}

func projectMutationResponse[T any](response providerResponse, operation string, successStatus int, notFound, rejected, defect, invalid sdkgo.BranchID) (sdkgo.MutationAttempt[T], bool) {
	if errors.Is(response.err, errCredential) {
		return projectMutationBranch[T](operation, defect, sdkgo.FailureAuthentication, "Basecamp credentials are unavailable"), true
	}
	if errors.Is(response.err, errRequest) {
		return projectMutationRetry[T](operation, sdkgo.FailureTransport, "Basecamp request failed", 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return projectMutationRetry[T](operation, sdkgo.FailureRateLimit, "Basecamp rate limited the request", response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return projectMutationRetry[T](operation, sdkgo.FailureAvailability, "Basecamp is temporarily unavailable", response.retryAfter), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return projectMutationBranch[T](operation, invalid, sdkgo.FailureResponseTooLarge, "Basecamp response exceeded the configured limit"), true
	}
	if response.err != nil {
		return projectMutationRetry[T](operation, sdkgo.FailureTransport, "Basecamp response was interrupted", 0), true
	}
	if response.status == http.StatusNotFound {
		return projectMutationBranch[T](operation, notFound, sdkgo.FailureNotFound, "Basecamp project does not exist"), true
	}
	if response.status != successStatus {
		return projectMutationBranch[T](operation, rejected, sdkgo.FailureProviderRejection, "Basecamp rejected HTTP "+strconv.Itoa(response.status)), true
	}
	return sdkgo.MutationAttempt[T]{}, false
}

func projectMutationBranch[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "basecamp", Operation: operation, Message: message}
	return sdkgo.NewMutationBranch(branch, *new(T), &failure, sdkgo.Receipt{})
}

func projectMutationRetry[T any](operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationRetry[T](sdkgo.Failure{Kind: kind, Provider: "basecamp", Operation: operation, Message: message}, delay)
}

func projectMutationUncertain[T any](operation, message string) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationUncertain(*new(T), sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "basecamp", Operation: operation, Message: message}, sdkgo.Receipt{})
}
