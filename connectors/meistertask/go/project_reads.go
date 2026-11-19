// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meistertask

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ProjectSummary contains the non-secret project identity and status.
type ProjectSummary struct {
	// ID is the positive MeisterTask project ID.
	ID string `json:"id"`
	// Name is the project name.
	Name string `json:"name"`
	// Status is the provider's decimal status code.
	Status string `json:"status"`
}

// ProjectsPage contains one ordered page of project summaries.
type ProjectsPage struct {
	// Projects are the projects visible to the token owner.
	Projects []ProjectSummary `json:"projects"`
	// Page is the requested one-based page number.
	Page int `json:"page"`
}

// ListProjectsInput selects one page of projects.
type ListProjectsInput struct {
	// Page is one-based; zero selects the first page.
	Page int `json:"page"`
	// Items is the positive page size; zero uses the provider default.
	Items int `json:"items"`
	// Status is active, archived, or all; blank uses active.
	Status string `json:"status"`
}

// GetProjectInput identifies one project.
type GetProjectInput struct {
	// ProjectID is a positive decimal ID from MeisterTask Projects.
	ProjectID string `json:"projectId"`
}

// ListProjectsOperation reads visible projects.
type ListProjectsOperation struct{ client *Client }

// ListProjects binds the Query to this client.
func (client *Client) ListProjects() ListProjectsOperation { return ListProjectsOperation{client} }

// Definition returns the generated project-list Query contract.
func (ListProjectsOperation) Definition() sdkgo.QueryDefinition { return ListProjectsDefinition }

// Invoke requests one project page and projects only non-secret fields.
func (operation ListProjectsOperation) Invoke(call sdkgo.Call, input ListProjectsInput) sdkgo.QueryAttempt[ProjectsPage] {
	if input.Page < 0 || input.Items < 0 || input.Items > 100 || (input.Status != "" && input.Status != "active" && input.Status != "archived" && input.Status != "all") {
		return projectQueryBranch("listProjects", ListProjectsBranchDefect, ProjectsPage{}, sdkgo.FailureValidation, "project page, size, or status is invalid")
	}
	page := input.Page
	if page == 0 {
		page = 1
	}
	query := url.Values{"page": {strconv.Itoa(page)}}
	if input.Items > 0 {
		query.Set("items", strconv.Itoa(input.Items))
	}
	if input.Status != "" {
		query.Set("status", input.Status)
	}
	response := operation.client.get(call, "/projects?"+query.Encode())
	if decision, handled := classifyProjectQueryResponse("listProjects", response, ProjectsPage{}, "", ListProjectsBranchProviderRejected, ListProjectsBranchInvalidResponse, ListProjectsBranchDefect); handled {
		return decision
	}
	var wire []json.RawMessage
	if err := json.Unmarshal(response.body, &wire); err != nil || wire == nil {
		return projectQueryBranch("listProjects", ListProjectsBranchInvalidResponse, ProjectsPage{}, sdkgo.FailureProtocol, "MeisterTask returned invalid project list JSON")
	}
	projects := make([]ProjectSummary, 0, len(wire))
	for _, raw := range wire {
		project, err := decodeProject(raw, "")
		if err != nil {
			return projectQueryBranch("listProjects", ListProjectsBranchInvalidResponse, ProjectsPage{}, sdkgo.FailureProtocol, "MeisterTask returned incomplete project metadata")
		}
		projects = append(projects, project)
	}
	return sdkgo.NewQueryBranch(ListProjectsBranchListed, ProjectsPage{Projects: projects, Page: page}, nil, sdkgo.Receipt{})
}

// GetProjectOperation reads one project.
type GetProjectOperation struct{ client *Client }

// GetProject binds the Query to this client.
func (client *Client) GetProject() GetProjectOperation { return GetProjectOperation{client} }

// Definition returns the generated project Query contract.
func (GetProjectOperation) Definition() sdkgo.QueryDefinition { return GetProjectDefinition }

// Invoke reads a project by ID.
func (operation GetProjectOperation) Invoke(call sdkgo.Call, input GetProjectInput) sdkgo.QueryAttempt[ProjectSummary] {
	if !validTaskID(input.ProjectID) {
		return projectQueryBranch("getProject", GetProjectBranchDefect, ProjectSummary{}, sdkgo.FailureValidation, "project ID must be a positive 32-bit decimal integer")
	}
	response := operation.client.get(call, "/projects/"+input.ProjectID)
	if decision, handled := classifyProjectQueryResponse("getProject", response, ProjectSummary{}, GetProjectBranchNotFound, GetProjectBranchProviderRejected, GetProjectBranchInvalidResponse, GetProjectBranchDefect); handled {
		return decision
	}
	project, err := decodeProject(response.body, input.ProjectID)
	if err != nil {
		return projectQueryBranch("getProject", GetProjectBranchInvalidResponse, ProjectSummary{}, sdkgo.FailureProtocol, "MeisterTask returned invalid project JSON")
	}
	return sdkgo.NewQueryBranch(GetProjectBranchFound, project, nil, sdkgo.Receipt{})
}

func decodeProject(body []byte, requestedID string) (ProjectSummary, error) {
	var wire struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Status *int64 `json:"status"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ProjectSummary{}, err
	}
	identifier := strconv.FormatInt(wire.ID, 10)
	if !validTaskID(identifier) || (requestedID != "" && requestedID != identifier) || wire.Name == "" || wire.Status == nil || (*wire.Status != 1 && *wire.Status != 4 && *wire.Status != 5) {
		return ProjectSummary{}, errors.New("project identity, name, or status is invalid")
	}
	return ProjectSummary{ID: identifier, Name: wire.Name, Status: strconv.FormatInt(*wire.Status, 10)}, nil
}

func classifyProjectQueryResponse[Output any](operation string, response providerResponse, empty Output, notFound, rejected, invalid, defect sdkgo.BranchID) (sdkgo.QueryAttempt[Output], bool) {
	if errors.Is(response.err, errCredential) {
		return projectQueryBranch(operation, defect, empty, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid"), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[Output](projectFailure(operation, sdkgo.FailureTransport, "MeisterTask request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[Output](projectFailure(operation, sdkgo.FailureRateLimit, "MeisterTask rate limited the request"), response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[Output](projectFailure(operation, sdkgo.FailureAvailability, "MeisterTask is temporarily unavailable"), response.retryAfter), true
	}
	if response.status == http.StatusNotFound && notFound != "" {
		return projectQueryBranch(operation, notFound, empty, sdkgo.FailureNotFound, "MeisterTask did not expose the project"), true
	}
	if response.status != http.StatusOK {
		return projectQueryBranch(operation, rejected, empty, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return projectQueryBranch(operation, invalid, empty, sdkgo.FailureResponseTooLarge, "MeisterTask response exceeded the configured limit"), true
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[Output](projectFailure(operation, sdkgo.FailureTransport, "MeisterTask response was interrupted"), 0), true
	}
	return sdkgo.QueryAttempt[Output]{}, false
}

func projectFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "meistertask", Operation: operation, Message: message}
}

func projectQueryBranch[Output any](operation string, branch sdkgo.BranchID, value Output, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[Output] {
	failure := projectFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}
