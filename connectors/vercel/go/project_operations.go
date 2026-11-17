// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package vercel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// Project contains public Vercel project metadata.
type Project struct {
	// ID is the stable Vercel project identifier.
	ID string `json:"id"`
	// Name is the current project name.
	Name string `json:"name"`
	// Framework is the configured framework when present.
	Framework string `json:"framework"`
}

// ProjectsPage contains one Vercel project page and its next cursor.
type ProjectsPage struct {
	// Projects contains the returned projects.
	Projects []Project `json:"projects"`
	// Pagination carries the next cursor when another page exists.
	Pagination struct {
		Next *int64 `json:"next"`
	} `json:"pagination"`
}

// ListProjectsInput selects a personal account or team.
type ListProjectsInput struct {
	// TeamID selects a team; blank selects the personal account.
	TeamID string `json:"teamId,omitempty"`
}

// GetProjectInput identifies one project and optional team.
type GetProjectInput struct {
	// ProjectID is the Vercel project ID or name.
	ProjectID string `json:"projectId"`
	// TeamID selects a team; blank selects the personal account.
	TeamID string `json:"teamId,omitempty"`
}

// CreateProjectInput supplies the exact project name and optional team.
type CreateProjectInput struct {
	// Name is the unique project name to create.
	Name string `json:"name"`
	// TeamID selects a team; blank selects the personal account.
	TeamID string `json:"teamId,omitempty"`
}

// UpdateProjectInput replaces one project's name.
type UpdateProjectInput struct {
	// ProjectID is the project ID or current name.
	ProjectID string `json:"projectId"`
	// Name is the replacement project name.
	Name string `json:"name"`
	// TeamID selects a team; blank selects the personal account.
	TeamID string `json:"teamId,omitempty"`
}

// DeleteProjectInput identifies one project to remove.
type DeleteProjectInput struct {
	// ProjectID is the project ID or name to remove.
	ProjectID string `json:"projectId"`
	// TeamID selects a team; blank selects the personal account.
	TeamID string `json:"teamId,omitempty"`
}

// DeletedProject identifies the project that is now absent.
type DeletedProject struct {
	// ProjectID identifies the project now absent.
	ProjectID string `json:"projectId"`
}

// ListProjectsOperation reads one page of projects.
type ListProjectsOperation struct{ client *Client }

// ListProjects binds project enumeration to this client.
func (client *Client) ListProjects() ListProjectsOperation { return ListProjectsOperation{client} }

// Definition returns the generated Query contract.
func (ListProjectsOperation) Definition() sdkgo.QueryDefinition { return ListProjectsDefinition }

// Invoke reads one project page.
func (operation ListProjectsOperation) Invoke(call sdkgo.Call, input ListProjectsInput) sdkgo.QueryAttempt[ProjectsPage] {
	if !validTeamID(input.TeamID) {
		return projectQueryFailure[ProjectsPage]("listProjects", "defect", sdkgo.FailureValidation)
	}
	return executeQuery(operation.client, call, "listProjects", requestSpec{method: http.MethodGet, path: "/v9/projects", query: teamQuery(input.TeamID)}, "listed", "", "providerRejected", "invalidResponse", "defect", decodeProjectsPage)
}

// GetProjectOperation reads one project.
type GetProjectOperation struct{ client *Client }

// GetProject binds a project lookup to this client.
func (client *Client) GetProject() GetProjectOperation { return GetProjectOperation{client} }

// Definition returns the generated Query contract.
func (GetProjectOperation) Definition() sdkgo.QueryDefinition { return GetProjectDefinition }

// Invoke reads a project by ID or name.
func (operation GetProjectOperation) Invoke(call sdkgo.Call, input GetProjectInput) sdkgo.QueryAttempt[Project] {
	if !validProjectIdentifier(input.ProjectID) || !validTeamID(input.TeamID) {
		return projectQueryFailure[Project]("getProject", "defect", sdkgo.FailureValidation)
	}
	return executeQuery(operation.client, call, "getProject", requestSpec{method: http.MethodGet, path: "/v9/projects/" + url.PathEscape(input.ProjectID), query: teamQuery(input.TeamID)}, "found", "notFound", "providerRejected", "invalidResponse", "defect", decodeProject)
}

// CreateProjectOperation creates a named project with bounded resend.
type CreateProjectOperation struct{ client *Client }

// CreateProject binds project creation to this client.
func (client *Client) CreateProject() CreateProjectOperation { return CreateProjectOperation{client} }

// Definition returns the generated Mutation contract.
func (CreateProjectOperation) Definition() sdkgo.MutationDefinition { return CreateProjectDefinition }

// IdempotencyKey identifies retries of one project creation.
func (CreateProjectOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one project, with uncertainty when the possible-send cap is reached.
func (operation CreateProjectOperation) Invoke(call sdkgo.Call, input CreateProjectInput) sdkgo.MutationAttempt[Project] {
	if !validProjectIdentifier(input.Name) || !validTeamID(input.TeamID) {
		return mutationFailure[Project]("createProject", "defect", sdkgo.FailureValidation, "Vercel project name or team ID is invalid")
	}
	body, err := json.Marshal(map[string]string{"name": input.Name})
	if err != nil {
		return mutationFailure[Project]("createProject", "defect", sdkgo.FailureLocalDefect, "Vercel project request could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateProjectDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return mutationFailure[Project]("createProject", "defect", sdkgo.FailureLocalDefect, "Vercel project dispatch could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return projectUncertain()
	}
	canResend := decision.MaximumPriorSends+1 < call.MaxSends()
	var response *http.Response
	var localFailure *sdkgo.Failure
	err = fence.Dispatch(func(requestContext context.Context) error {
		var sendErr error
		response, localFailure, sendErr = operation.client.sendWithContext(call, requestContext, "createProject", requestSpec{method: http.MethodPost, path: "/v9/projects", query: teamQuery(input.TeamID), contentType: "application/json", body: body}, true)
		return sendErr
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return mutationRetry[Project]("createProject", sdkgo.FailureAvailability, 0)
	}
	if localFailure != nil {
		return projectUncertain()
	}
	if response == nil {
		if canResend {
			return mutationRetry[Project]("createProject", sdkgo.FailureTransport, 0)
		}
		return projectUncertain()
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return mutationRetry[Project]("createProject", sdkgo.FailureAvailability, 0)
		}
		kind, delay := retryFailure(response, "createProject")
		return sdkgo.NewMutationRetry[Project](kind, delay)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 || err != nil {
		if canResend {
			return mutationRetry[Project]("createProject", sdkgo.FailureAvailability, 0)
		}
		return projectUncertain()
	}
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return projectUncertain()
		}
		return mutationFailure[Project]("createProject", "providerRejected", sdkgo.FailureProviderRejection, "Vercel rejected project creation")
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return projectUncertain()
	}
	project, err := decodeProject(responseBody)
	if err != nil || project.Name != input.Name {
		return projectUncertain()
	}
	return sdkgo.NewMutationBranch(sdkgo.BranchID("created"), project, nil, sdkgo.Receipt{})
}

// UpdateProjectOperation renames one project.
type UpdateProjectOperation struct{ client *Client }

// UpdateProject binds an absolute rename to this client.
func (client *Client) UpdateProject() UpdateProjectOperation { return UpdateProjectOperation{client} }

// Definition returns the generated Mutation contract.
func (UpdateProjectOperation) Definition() sdkgo.MutationDefinition { return UpdateProjectDefinition }

// IdempotencyKey identifies retries of one rename.
func (UpdateProjectOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the project's current name.
func (operation UpdateProjectOperation) Invoke(call sdkgo.Call, input UpdateProjectInput) sdkgo.MutationAttempt[Project] {
	if !validProjectIdentifier(input.ProjectID) || !validProjectIdentifier(input.Name) || !validTeamID(input.TeamID) {
		return mutationFailure[Project]("updateProject", "defect", sdkgo.FailureValidation, "Vercel project ID, name, or team ID is invalid")
	}
	body, err := json.Marshal(map[string]string{"name": input.Name})
	if err != nil {
		return mutationFailure[Project]("updateProject", "defect", sdkgo.FailureLocalDefect, "Vercel project request could not be encoded")
	}
	response, localFailure, err := operation.client.send(call, "updateProject", requestSpec{method: http.MethodPatch, path: "/v9/projects/" + url.PathEscape(input.ProjectID), query: teamQuery(input.TeamID), contentType: "application/json", body: body})
	if localFailure != nil {
		return sdkgo.NewMutationBranch(sdkgo.BranchID("defect"), Project{}, localFailure, sdkgo.Receipt{})
	}
	if err != nil {
		return mutationRetry[Project]("updateProject", sdkgo.FailureTransport, 0)
	}
	defer func() { _ = response.Body.Close() }()
	if shouldRetry(response) {
		kind, delay := retryFailure(response, "updateProject")
		return sdkgo.NewMutationRetry[Project](kind, delay)
	}
	if response.StatusCode == http.StatusNotFound {
		return mutationFailure[Project]("updateProject", "notFound", sdkgo.FailureNotFound, "Vercel did not find the project")
	}
	if response.StatusCode != http.StatusOK {
		return mutationFailure[Project]("updateProject", "providerRejected", sdkgo.FailureProviderRejection, "Vercel rejected project update")
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return mutationFailure[Project]("updateProject", "invalidResponse", sdkgo.FailureResponseTooLarge, "Vercel project response was too large")
	}
	if err != nil {
		return mutationRetry[Project]("updateProject", sdkgo.FailureTransport, 0)
	}
	project, err := decodeProject(responseBody)
	if err != nil || project.Name != input.Name {
		return mutationFailure[Project]("updateProject", "invalidResponse", sdkgo.FailureProtocol, "Vercel project response was malformed")
	}
	return sdkgo.NewMutationBranch(sdkgo.BranchID("updated"), project, nil, sdkgo.Receipt{})
}

// DeleteProjectOperation removes one project.
type DeleteProjectOperation struct{ client *Client }

// DeleteProject binds deletion to this client.
func (client *Client) DeleteProject() DeleteProjectOperation { return DeleteProjectOperation{client} }

// Definition returns the generated Mutation contract.
func (DeleteProjectOperation) Definition() sdkgo.MutationDefinition { return DeleteProjectDefinition }

// IdempotencyKey identifies retries of one deletion.
func (DeleteProjectOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes the project; an absent project already meets the target state.
func (operation DeleteProjectOperation) Invoke(call sdkgo.Call, input DeleteProjectInput) sdkgo.MutationAttempt[DeletedProject] {
	if !validProjectIdentifier(input.ProjectID) || !validTeamID(input.TeamID) {
		return mutationFailure[DeletedProject]("deleteProject", "defect", sdkgo.FailureValidation, "Vercel project ID or team ID is invalid")
	}
	response, localFailure, err := operation.client.send(call, "deleteProject", requestSpec{method: http.MethodDelete, path: "/v9/projects/" + url.PathEscape(input.ProjectID), query: teamQuery(input.TeamID)})
	if localFailure != nil {
		return sdkgo.NewMutationBranch(sdkgo.BranchID("defect"), DeletedProject{}, localFailure, sdkgo.Receipt{})
	}
	if err != nil {
		return mutationRetry[DeletedProject]("deleteProject", sdkgo.FailureTransport, 0)
	}
	defer func() { _ = response.Body.Close() }()
	if shouldRetry(response) {
		kind, delay := retryFailure(response, "deleteProject")
		return sdkgo.NewMutationRetry[DeletedProject](kind, delay)
	}
	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNotFound {
		return mutationFailure[DeletedProject]("deleteProject", "providerRejected", sdkgo.FailureProviderRejection, "Vercel rejected project deletion")
	}
	return sdkgo.NewMutationBranch(sdkgo.BranchID("deleted"), DeletedProject{ProjectID: input.ProjectID}, nil, sdkgo.Receipt{})
}

func validProjectIdentifier(value string) bool {
	return value != "" && len(value) <= 128 && !strings.ContainsAny(value, "/?# ")
}
func decodeProject(body []byte) (Project, error) {
	var project Project
	if err := json.Unmarshal(body, &project); err != nil {
		return Project{}, err
	}
	if project.ID == "" || project.Name == "" {
		return Project{}, errors.New("missing project identity")
	}
	return project, nil
}
func decodeProjectsPage(body []byte) (ProjectsPage, error) {
	var page ProjectsPage
	if err := json.Unmarshal(body, &page); err != nil {
		return ProjectsPage{}, err
	}
	if page.Projects == nil {
		return ProjectsPage{}, errors.New("missing projects")
	}
	for _, project := range page.Projects {
		if project.ID == "" || project.Name == "" {
			return ProjectsPage{}, errors.New("invalid project")
		}
	}
	return page, nil
}
func projectQueryFailure[T any](operation, branch string, kind sdkgo.FailureKind) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "vercel", Operation: operation, Message: "Vercel project request is invalid"}
	return sdkgo.NewQueryBranch(sdkgo.BranchID(branch), *new(T), &failure, sdkgo.Receipt{})
}
func projectUncertain() sdkgo.MutationAttempt[Project] {
	failure := sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "vercel", Operation: "createProject", Message: "Vercel project may have been created but could not be confirmed"}
	return sdkgo.NewMutationUncertain(Project{}, failure, sdkgo.Receipt{})
}
