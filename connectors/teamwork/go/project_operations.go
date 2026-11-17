// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teamwork

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// ListProjectsInput selects one bounded project page.
type ListProjectsInput struct {
	// Page is one-based; zero selects the first page.
	Page int `json:"page,omitempty"`
	// PageSize is from 1 through 500; zero selects 50.
	PageSize int `json:"pageSize,omitempty"`
}

// GetProjectInput identifies one Teamwork project.
type GetProjectInput struct {
	// ProjectID is the numeric Teamwork project ID.
	ProjectID string `json:"projectId"`
}

// CreateProjectInput names a new project.
type CreateProjectInput struct {
	// Name is the nonempty project name.
	Name string `json:"name"`
}

// UpdateProjectInput replaces a project's name.
type UpdateProjectInput struct {
	// ProjectID is the numeric Teamwork project ID.
	ProjectID string `json:"projectId"`
	// Name is the replacement project name.
	Name string `json:"name"`
}

// ProjectSummary excludes project descriptions, client identity, and member data.
type ProjectSummary struct {
	// ID is the Teamwork project ID.
	ID string `json:"id"`
	// Name is the project name.
	Name string `json:"name"`
	// Status is the Teamwork project state.
	Status string `json:"status"`
}

// ProjectPage holds one project page without private project details.
type ProjectPage struct {
	// Projects are project summaries.
	Projects []ProjectSummary `json:"projects"`
	// Page is the requested one-based page.
	Page int `json:"page"`
}

// ProjectID is the identity returned by a project write.
type ProjectID struct {
	// ID is the Teamwork project ID.
	ID string `json:"id"`
}

// ListProjectsOperation reads accessible projects.
type ListProjectsOperation struct{ client *Client }

// ListProjects binds the query to this client.
func (client *Client) ListProjects() ListProjectsOperation { return ListProjectsOperation{client} }

// Definition returns the generated query contract.
func (ListProjectsOperation) Definition() sdkgo.QueryDefinition { return ListProjectsDefinition }

// Invoke reads a bounded project page.
func (operation ListProjectsOperation) Invoke(call sdkgo.Call, input ListProjectsInput) sdkgo.QueryAttempt[ProjectPage] {
	if input.Page < 0 || input.PageSize < 0 || input.PageSize > 500 {
		return taskQueryBranch(ListProjectsBranchDefect, ProjectPage{}, "listProjects", sdkgo.FailureValidation, "invalid project page")
	}
	if input.Page == 0 {
		input.Page = 1
	}
	if input.PageSize == 0 {
		input.PageSize = 50
	}
	query := url.Values{"page": {strconv.Itoa(input.Page)}, "pageSize": {strconv.Itoa(input.PageSize)}}
	response := operation.client.get(call, "/projects/api/v3/projects.json?"+query.Encode())
	if retry, failure := taskResponseRetry("listProjects", response); retry {
		return sdkgo.NewQueryRetry[ProjectPage](failure, response.retryAfter)
	}
	if response.err == errCredential {
		return taskQueryBranch(ListProjectsBranchDefect, ProjectPage{}, "listProjects", sdkgo.FailureAuthentication, "credentials unavailable")
	}
	if response.status != http.StatusOK {
		return taskQueryBranch(ListProjectsBranchProviderRejected, ProjectPage{}, "listProjects", sdkgo.FailureProviderRejection, "Teamwork rejected project listing")
	}
	if response.err != nil {
		return taskQueryBranch(ListProjectsBranchInvalidResponse, ProjectPage{}, "listProjects", sdkgo.FailureResponseTooLarge, "project page exceeded response limit")
	}
	var wire struct {
		Projects []projectWire `json:"projects"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Projects == nil {
		return taskQueryBranch(ListProjectsBranchInvalidResponse, ProjectPage{}, "listProjects", sdkgo.FailureProtocol, "project page invalid")
	}
	page := ProjectPage{Projects: make([]ProjectSummary, 0, len(wire.Projects)), Page: input.Page}
	for _, project := range wire.Projects {
		summary, err := project.summary()
		if err != nil {
			return taskQueryBranch(ListProjectsBranchInvalidResponse, ProjectPage{}, "listProjects", sdkgo.FailureProtocol, "project page contains invalid project")
		}
		page.Projects = append(page.Projects, summary)
	}
	return sdkgo.NewQueryBranch(ListProjectsBranchListed, page, nil, sdkgo.Receipt{})
}

// GetProjectOperation reads one project.
type GetProjectOperation struct{ client *Client }

// GetProject binds the query to this client.
func (client *Client) GetProject() GetProjectOperation { return GetProjectOperation{client} }

// Definition returns the generated query contract.
func (GetProjectOperation) Definition() sdkgo.QueryDefinition { return GetProjectDefinition }

// Invoke reads project identity, name, and status.
func (operation GetProjectOperation) Invoke(call sdkgo.Call, input GetProjectInput) sdkgo.QueryAttempt[ProjectSummary] {
	if !validTaskID(input.ProjectID) {
		return taskQueryBranch(GetProjectBranchDefect, ProjectSummary{}, "getProject", sdkgo.FailureValidation, "project ID invalid")
	}
	response := operation.client.get(call, "/projects/api/v3/projects/"+input.ProjectID+".json")
	if retry, failure := taskResponseRetry("getProject", response); retry {
		return sdkgo.NewQueryRetry[ProjectSummary](failure, response.retryAfter)
	}
	if response.err == errCredential {
		return taskQueryBranch(GetProjectBranchDefect, ProjectSummary{}, "getProject", sdkgo.FailureAuthentication, "credentials unavailable")
	}
	if response.status == http.StatusNotFound {
		return taskQueryBranch(GetProjectBranchNotFound, ProjectSummary{}, "getProject", sdkgo.FailureNotFound, "project not found")
	}
	if response.status != http.StatusOK {
		return taskQueryBranch(GetProjectBranchProviderRejected, ProjectSummary{}, "getProject", sdkgo.FailureProviderRejection, "Teamwork rejected project lookup")
	}
	if response.err != nil {
		return taskQueryBranch(GetProjectBranchInvalidResponse, ProjectSummary{}, "getProject", sdkgo.FailureResponseTooLarge, "project response exceeded limit")
	}
	var wire struct {
		Project projectWire `json:"project"`
	}
	if json.Unmarshal(response.body, &wire) != nil {
		return taskQueryBranch(GetProjectBranchInvalidResponse, ProjectSummary{}, "getProject", sdkgo.FailureProtocol, "project response invalid")
	}
	summary, err := wire.Project.summary()
	if err != nil || summary.ID != input.ProjectID {
		return taskQueryBranch(GetProjectBranchInvalidResponse, ProjectSummary{}, "getProject", sdkgo.FailureProtocol, "project identity mismatch")
	}
	return sdkgo.NewQueryBranch(GetProjectBranchFound, summary, nil, sdkgo.Receipt{})
}

// CreateProjectOperation creates one project with a bounded resend cap.
type CreateProjectOperation struct{ client *Client }

// CreateProject binds the mutation to this client.
func (client *Client) CreateProject() CreateProjectOperation { return CreateProjectOperation{client} }

// Definition returns the generated mutation contract.
func (CreateProjectOperation) Definition() sdkgo.MutationDefinition { return CreateProjectDefinition }

// IdempotencyKey derives stable Dex call identity; Teamwork does not deduplicate it.
func (CreateProjectOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a project and returns uncertainty after unconfirmed sends exhaust the cap.
func (operation CreateProjectOperation) Invoke(call sdkgo.Call, input CreateProjectInput) sdkgo.MutationAttempt[ProjectID] {
	if strings.TrimSpace(input.Name) == "" {
		return taskMutationBranch(CreateProjectBranchDefect, ProjectID{}, "createProject", sdkgo.FailureValidation, "project name required")
	}
	body, err := json.Marshal(map[string]any{"project": map[string]string{"name": input.Name}})
	if err != nil {
		return taskMutationBranch(CreateProjectBranchDefect, ProjectID{}, "createProject", sdkgo.FailureValidation, "project name cannot be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateProjectDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return taskMutationBranch(CreateProjectBranchDefect, ProjectID{}, "createProject", sdkgo.FailureLocalDefect, "create checkpoint unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return taskMutationBranch(CreateProjectBranchDefect, ProjectID{}, "createProject", sdkgo.FailureLocalDefect, "create checkpoint invalid")
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return projectCreateUncertain("project create could not be confirmed")
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.request(call, requestContext, http.MethodPost, "/projects.json", body)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[ProjectID](taskOperationFailure("createProject", sdkgo.FailureAvailability, "project create outcome unknown"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[ProjectID](taskOperationFailure("createProject", sdkgo.FailureAvailability, "retry checkpoint unavailable"), 0)
		}
	}
	if retry, failure := taskResponseRetry("createProject", response); retry {
		if response.status != http.StatusTooManyRequests && response.status != http.StatusServiceUnavailable && decision.MaximumPriorSends+1 >= call.MaxSends() {
			return projectCreateUncertain("project create could not be confirmed")
		}
		return sdkgo.NewMutationRetry[ProjectID](failure, response.retryAfter)
	}
	if response.err == errCredential {
		return taskMutationBranch(CreateProjectBranchDefect, ProjectID{}, "createProject", sdkgo.FailureAuthentication, "credentials unavailable")
	}
	if response.status >= 400 {
		return taskMutationBranch(CreateProjectBranchProviderRejected, ProjectID{}, "createProject", sdkgo.FailureProviderRejection, "Teamwork rejected project create")
	}
	if response.status != http.StatusCreated {
		return projectCreateRetryOrUncertain(call, decision.MaximumPriorSends, "project create response unconfirmed")
	}
	var wire struct {
		ID     string `json:"id"`
		Status string `json:"STATUS"`
	}
	if json.Unmarshal(response.body, &wire) != nil || !validTaskID(wire.ID) || wire.Status != "Created" {
		return projectCreateRetryOrUncertain(call, decision.MaximumPriorSends, "project create response invalid")
	}
	return sdkgo.NewMutationBranch(CreateProjectBranchCreated, ProjectID{ID: wire.ID}, nil, sdkgo.Receipt{})
}

// UpdateProjectOperation replaces a project name.
type UpdateProjectOperation struct{ client *Client }

// UpdateProject binds the mutation to this client.
func (client *Client) UpdateProject() UpdateProjectOperation { return UpdateProjectOperation{client} }

// Definition returns the generated mutation contract.
func (UpdateProjectOperation) Definition() sdkgo.MutationDefinition { return UpdateProjectDefinition }

// IdempotencyKey derives stable identity for absolute name replacement.
func (UpdateProjectOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces a project name and checks Teamwork's V1 status response.
func (operation UpdateProjectOperation) Invoke(call sdkgo.Call, input UpdateProjectInput) sdkgo.MutationAttempt[ProjectID] {
	if !validTaskID(input.ProjectID) || strings.TrimSpace(input.Name) == "" {
		return taskMutationBranch(UpdateProjectBranchDefect, ProjectID{}, "updateProject", sdkgo.FailureValidation, "project ID and name required")
	}
	body, err := json.Marshal(map[string]any{"project": map[string]string{"name": input.Name}})
	if err != nil {
		return taskMutationBranch(UpdateProjectBranchDefect, ProjectID{}, "updateProject", sdkgo.FailureValidation, "project name cannot be encoded")
	}
	response := operation.client.request(call, call.Context, http.MethodPut, "/projects/"+input.ProjectID+".json", body)
	if retry, failure := taskResponseRetry("updateProject", response); retry {
		return sdkgo.NewMutationRetry[ProjectID](failure, response.retryAfter)
	}
	if response.err == errCredential {
		return taskMutationBranch(UpdateProjectBranchDefect, ProjectID{}, "updateProject", sdkgo.FailureAuthentication, "credentials unavailable")
	}
	if response.status == http.StatusNotFound {
		return taskMutationBranch(UpdateProjectBranchNotFound, ProjectID{}, "updateProject", sdkgo.FailureNotFound, "project not found")
	}
	if response.status != http.StatusOK {
		return taskMutationBranch(UpdateProjectBranchProviderRejected, ProjectID{}, "updateProject", sdkgo.FailureProviderRejection, "Teamwork rejected project update")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return taskMutationBranch(UpdateProjectBranchInvalidResponse, ProjectID{}, "updateProject", sdkgo.FailureResponseTooLarge, "project response exceeded limit")
	}
	var wire struct {
		Status string `json:"STATUS"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Status != "OK" {
		return taskMutationBranch(UpdateProjectBranchInvalidResponse, ProjectID{}, "updateProject", sdkgo.FailureProtocol, "project response invalid")
	}
	return sdkgo.NewMutationBranch(UpdateProjectBranchUpdated, ProjectID{ID: input.ProjectID}, nil, sdkgo.Receipt{})
}

type projectWire struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

func (project projectWire) summary() (ProjectSummary, error) {
	if project.ID <= 0 || project.ID > 9007199254740991 || strings.TrimSpace(project.Name) == "" || strings.TrimSpace(project.Status) == "" {
		return ProjectSummary{}, errors.New("project identity missing")
	}
	return ProjectSummary{ID: strconv.FormatInt(project.ID, 10), Name: project.Name, Status: project.Status}, nil
}

func projectCreateUncertain(message string) sdkgo.MutationAttempt[ProjectID] {
	return sdkgo.NewMutationUncertain(ProjectID{}, taskOperationFailure("createProject", sdkgo.FailureAvailability, message), sdkgo.Receipt{})
}

func projectCreateRetryOrUncertain(call sdkgo.Call, previousSends int32, message string) sdkgo.MutationAttempt[ProjectID] {
	if previousSends+1 >= call.MaxSends() {
		return projectCreateUncertain(message)
	}
	return sdkgo.NewMutationRetry[ProjectID](taskOperationFailure("createProject", sdkgo.FailureProtocol, message), 0)
}
