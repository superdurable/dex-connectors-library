// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jihugitlab

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const projectsPath = "/api/v4/projects"

// ListMemberProjectsInput selects a one-based project page; zero values use page 1 and 20 projects.
type ListMemberProjectsInput struct {
	// Page is the one-based project page number.
	Page int `json:"page"`
	// PerPage is the number of projects requested, from 1 to 100.
	PerPage int `json:"perPage"`
}

// MemberProject contains only stable Jihu GitLab project metadata.
type MemberProject struct {
	// ID is the project ID.
	ID int64 `json:"id"`
	// Name is the project name.
	Name string `json:"name"`
	// Path is the project path with namespace.
	Path string `json:"path"`
}

// ListMemberProjectsOutput contains one page and the next page number, or zero at the end.
type ListMemberProjectsOutput struct {
	// Projects are the returned member projects in provider order.
	Projects []MemberProject `json:"projects"`
	// NextPage is the next one-based page number, or zero if the page is terminal.
	NextPage int `json:"nextPage"`
}

// ListMemberProjectsOperation reads the Jihu GitLab project directory.
type ListMemberProjectsOperation struct{ client *Client }

// Definition returns the generated member-project contract.
func (ListMemberProjectsOperation) Definition() sdkgo.QueryDefinition {
	return ListMemberProjectsDefinition
}

// Invoke lists member projects and classifies provider and transport outcomes.
func (operation ListMemberProjectsOperation) Invoke(call sdkgo.Call, input ListMemberProjectsInput) sdkgo.QueryAttempt[ListMemberProjectsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return projectCatalogBranch(ListMemberProjectsBranchDefect, sdkgo.FailureAuthentication, "Jihu GitLab token is unavailable or invalid")
	}
	page, perPage := input.Page, input.PerPage
	if page == 0 {
		page = 1
	}
	if perPage == 0 {
		perPage = 20
	}
	if page < 1 || page > 50000 || perPage < 1 || perPage > 100 {
		return projectCatalogBranch(ListMemberProjectsBranchDefect, sdkgo.FailureValidation, "page or perPage is outside the supported range")
	}
	values := url.Values{}
	values.Set("membership", "true")
	values.Set("page", strconv.Itoa(page))
	values.Set("per_page", strconv.Itoa(perPage))
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+projectsPath+"?"+values.Encode(), nil)
	if err != nil {
		return projectCatalogBranch(ListMemberProjectsBranchDefect, sdkgo.FailureValidation, "project-list request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("PRIVATE-TOKEN", credentials.APIKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return projectCatalogRetry(sdkgo.FailureAvailability, "Jihu GitLab request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return projectCatalogRetry(sdkgo.FailureRateLimit, "Jihu GitLab rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return projectCatalogRetry(sdkgo.FailureAvailability, "Jihu GitLab temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return projectCatalogBranch(ListMemberProjectsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "project response exceeded the configured limit")
	}
	if err != nil {
		return projectCatalogRetry(sdkgo.FailureAvailability, "project response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return projectCatalogBranch(ListMemberProjectsBranchProviderRejected, kind, "Jihu GitLab rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		Path string `json:"path_with_namespace"`
	}
	if json.Unmarshal(body, &wire) != nil || wire == nil || len(wire) > perPage {
		return projectCatalogBranch(ListMemberProjectsBranchInvalidResponse, sdkgo.FailureProtocol, "Jihu GitLab returned invalid project data")
	}
	projects := make([]MemberProject, 0, len(wire))
	for _, item := range wire {
		if item.ID <= 0 || item.Name == "" || len(item.Name) > 512 || item.Path == "" || len(item.Path) > 1024 || strings.TrimSpace(item.Path) != item.Path {
			return projectCatalogBranch(ListMemberProjectsBranchInvalidResponse, sdkgo.FailureProtocol, "Jihu GitLab returned an invalid project")
		}
		projects = append(projects, MemberProject{ID: item.ID, Name: item.Name, Path: item.Path})
	}
	nextPage := 0
	if header := response.Header.Get("X-Next-Page"); header != "" {
		nextPage, err = strconv.Atoi(header)
		if err != nil || nextPage <= page || nextPage > 50000 {
			return projectCatalogBranch(ListMemberProjectsBranchInvalidResponse, sdkgo.FailureProtocol, "Jihu GitLab returned invalid pagination data")
		}
	}
	return sdkgo.NewQueryBranch(ListMemberProjectsBranchListed, ListMemberProjectsOutput{Projects: projects, NextPage: nextPage}, nil, sdkgo.Receipt{})
}
func isValidAPIKey(value string) bool {
	if value == "" || len(value) > 512 {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func projectCatalogBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListMemberProjectsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "jihugitlab", Operation: "listMemberProjects", Message: message}
	return sdkgo.NewQueryBranch(branch, ListMemberProjectsOutput{}, &failure, sdkgo.Receipt{})
}
func projectCatalogRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListMemberProjectsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "jihugitlab", Operation: "listMemberProjects", Message: message}
	return sdkgo.NewQueryRetry[ListMemberProjectsOutput](failure, delay)
}
