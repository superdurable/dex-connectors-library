// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package copper

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListProjectsInput selects one Copper search page with optional filters.
type ListProjectsInput struct {
	// PageNumber starts at one; zero uses Copper's first page.
	PageNumber int `json:"pageNumber,omitempty"`
	// PageSize is 1 through 200; zero uses Copper's default of 20.
	PageSize int `json:"pageSize,omitempty"`
	// Name optionally matches a full project name.
	Name string `json:"name,omitempty"`
}

// ProjectsPage contains one returned page of project summaries.
type ProjectsPage struct {
	// Projects are the records returned by Copper for this page.
	Projects []ProjectMetadata `json:"projects"`
}

// ListProjectsOperation searches Copper projects without changing them.
type ListProjectsOperation struct{ client *Client }

// ListProjects binds the Query to this client.
func (client *Client) ListProjects() ListProjectsOperation {
	return ListProjectsOperation{client}
}

// Definition returns the generated Query contract.
func (ListProjectsOperation) Definition() sdkgo.QueryDefinition {
	return ListProjectsDefinition
}

// Invoke reads one page through Copper's projects search endpoint.
func (operation ListProjectsOperation) Invoke(call sdkgo.Call, input ListProjectsInput) sdkgo.QueryAttempt[ProjectsPage] {
	if input.PageNumber < 0 || input.PageNumber > 5000 || input.PageSize < 0 || input.PageSize > 200 ||
		len(input.Name) > 255 || strings.ContainsAny(input.Name, "\r\n") {
		return listProjectsBranch(ListProjectsBranchDefect, sdkgo.FailureValidation, "invalid projects page or filter")
	}
	request := struct {
		PageNumber int    `json:"page_number,omitempty"`
		PageSize   int    `json:"page_size,omitempty"`
		Name       string `json:"name,omitempty"`
	}{PageNumber: input.PageNumber, PageSize: input.PageSize, Name: input.Name}
	requestBody, err := json.Marshal(request)
	if err != nil {
		return listProjectsBranch(ListProjectsBranchDefect, sdkgo.FailureLocalDefect, "projects search could not be encoded")
	}
	response := operation.client.personRequest(call.Context, call, http.MethodPost, "/projects/search", requestBody)
	if errors.Is(response.err, errCredential) {
		return listProjectsBranch(ListProjectsBranchDefect, sdkgo.FailureAuthentication, "Copper credentials are unavailable or invalid")
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[ProjectsPage](queryFailure("listProjects", sdkgo.FailureRateLimit, "Copper rate limited projects search"), response.retryAfter)
	}
	if response.status == http.StatusServiceUnavailable || response.status >= 500 || errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[ProjectsPage](queryFailure("listProjects", sdkgo.FailureAvailability, "Copper projects search is unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return listProjectsBranch(ListProjectsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Copper projects response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[ProjectsPage](queryFailure("listProjects", sdkgo.FailureTransport, "Copper projects response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return listProjectsBranch(ListProjectsBranchProviderRejected, sdkgo.FailureProviderRejection, "Copper rejected HTTP "+strconv.Itoa(response.status))
	}
	var records []json.RawMessage
	if json.Unmarshal(response.body, &records) != nil || records == nil {
		return listProjectsBranch(ListProjectsBranchInvalidResponse, sdkgo.FailureProtocol, "Copper returned invalid projects JSON")
	}
	page := ProjectsPage{Projects: make([]ProjectMetadata, 0, len(records))}
	for _, record := range records {
		project, decodeError := decodeProject(record)
		if decodeError != nil {
			return listProjectsBranch(ListProjectsBranchInvalidResponse, sdkgo.FailureProtocol, "Copper returned an invalid project")
		}
		page.Projects = append(page.Projects, project)
	}
	return sdkgo.NewQueryBranch(ListProjectsBranchListed, page, nil, sdkgo.Receipt{})
}

func listProjectsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ProjectsPage] {
	failure := queryFailure("listProjects", kind, message)
	return sdkgo.NewQueryBranch(branch, ProjectsPage{}, &failure, sdkgo.Receipt{})
}
