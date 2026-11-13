// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zadig

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

// ListProjectsInput selects one project page. Zero page number and size use 1 and 20.
type ListProjectsInput struct {
	// PageNum is the one-based page number; zero selects the first page.
	PageNum int `json:"pageNum"`
	// PageSize is the number of projects to request, from 1 to 100; zero uses 20.
	PageSize int `json:"pageSize"`
}

// Project contains non-secret Zadig project metadata.
type Project struct {
	// Key is the stable project identifier.
	Key string `json:"key"`
	// Name is the project display name.
	Name string `json:"name"`
	// DeployType is the provider's project deployment type.
	DeployType string `json:"deployType"`
}

// ListProjectsOutput contains one project page and the total project count.
type ListProjectsOutput struct {
	// Projects are the returned projects in provider order.
	Projects []Project `json:"projects"`
	// Total is the total count reported by Zadig.
	Total int `json:"total"`
}

// ListProjectsOperation reads one Zadig project page.
type ListProjectsOperation struct{ client *Client }

// Definition returns the generated project-list contract.
func (ListProjectsOperation) Definition() sdkgo.QueryDefinition { return ListProjectsDefinition }

// Invoke reads one project page and classifies provider and transport outcomes.
func (operation ListProjectsOperation) Invoke(call sdkgo.Call, input ListProjectsInput) sdkgo.QueryAttempt[ListProjectsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return projectCatalogBranch(ListProjectsBranchDefect, sdkgo.FailureAuthentication, "connection API token is unavailable or invalid")
	}
	pageNum, pageSize := input.PageNum, input.PageSize
	if pageNum == 0 {
		pageNum = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	if pageNum < 1 || pageNum > 1000000 || pageSize < 1 || pageSize > 100 {
		return projectCatalogBranch(ListProjectsBranchDefect, sdkgo.FailureValidation, "page number or size is outside the supported range")
	}
	parameters := url.Values{}
	parameters.Set("pageNum", strconv.Itoa(pageNum))
	parameters.Set("pageSize", strconv.Itoa(pageSize))
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/openapi/projects/project?"+parameters.Encode(), nil)
	if err != nil {
		return projectCatalogBranch(ListProjectsBranchDefect, sdkgo.FailureValidation, "project-list request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return projectCatalogRetry(sdkgo.FailureAvailability, "Zadig request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return projectCatalogRetry(sdkgo.FailureRateLimit, "Zadig rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return projectCatalogRetry(sdkgo.FailureAvailability, "Zadig is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return projectCatalogBranch(ListProjectsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Zadig response exceeded the configured limit")
	}
	if err != nil {
		return projectCatalogRetry(sdkgo.FailureAvailability, "Zadig response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return projectCatalogBranch(ListProjectsBranchProviderRejected, kind, "Zadig rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		Total    *int `json:"total"`
		Projects []struct {
			Key        string `json:"project_key"`
			Name       string `json:"project_name"`
			DeployType string `json:"deploy_type"`
		} `json:"projects"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Total == nil || *wire.Total < 0 || wire.Projects == nil || len(wire.Projects) > pageSize {
		return projectCatalogBranch(ListProjectsBranchInvalidResponse, sdkgo.FailureProtocol, "Zadig returned invalid project-list data")
	}
	projects := make([]Project, 0, len(wire.Projects))
	for _, item := range wire.Projects {
		if item.Key == "" || len(item.Key) > 256 || strings.TrimSpace(item.Key) != item.Key || item.Name == "" || len(item.Name) > 512 || len(item.DeployType) > 64 {
			return projectCatalogBranch(ListProjectsBranchInvalidResponse, sdkgo.FailureProtocol, "Zadig returned an invalid project")
		}
		projects = append(projects, Project{Key: item.Key, Name: item.Name, DeployType: item.DeployType})
	}
	return sdkgo.NewQueryBranch(ListProjectsBranchListed, ListProjectsOutput{Projects: projects, Total: *wire.Total}, nil, sdkgo.Receipt{})
}

func isValidAPIKey(value string) bool {
	if value == "" || len(value) > 512 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func projectCatalogBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListProjectsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "zadig", Operation: "listProjects", Message: message}
	return sdkgo.NewQueryBranch(branch, ListProjectsOutput{}, &failure, sdkgo.Receipt{})
}
func projectCatalogRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListProjectsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "zadig", Operation: "listProjects", Message: message}
	return sdkgo.NewQueryRetry[ListProjectsOutput](failure, delay)
}
