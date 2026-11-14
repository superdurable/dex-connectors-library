// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package worktile

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListProjectsInput selects one zero-based page of projects.
type ListProjectsInput struct {
	// PageIndex starts at zero.
	PageIndex int `json:"pageIndex"`
	// PageSize is the number of projects to request, from one through one hundred.
	PageSize int `json:"pageSize"`
}

// Project contains non-personal Worktile project metadata.
type Project struct {
	// ID is the project identifier.
	ID string `json:"id"`
	// Name is the project name.
	Name string `json:"name"`
	// Identifier is the project code.
	Identifier string `json:"identifier"`
	// Visibility is one for public and two for private projects.
	Visibility int `json:"visibility"`
}

// ListProjectsOutput contains one page of projects.
type ListProjectsOutput struct {
	// PageIndex is the provider's zero-based page number.
	PageIndex int `json:"pageIndex"`
	// PageSize is the provider's page size.
	PageSize int `json:"pageSize"`
	// HasMore indicates another page is available.
	HasMore bool `json:"hasMore"`
	// Projects contains the projects in this page.
	Projects []Project `json:"projects"`
}

// ListProjectsOperation queries the fixed HTTPS project-list endpoint.
type ListProjectsOperation struct{ client *Client }

// Definition returns the generated project-list contract.
func (ListProjectsOperation) Definition() sdkgo.QueryDefinition { return ListProjectsDefinition }

// Invoke obtains a short-lived tenant token and lists one page without persisting credentials or URLs.
func (operation ListProjectsOperation) Invoke(call sdkgo.Call, input ListProjectsInput) sdkgo.QueryAttempt[ListProjectsOutput] {
	if input.PageIndex < 0 || input.PageSize < 1 || input.PageSize > 100 {
		return projectBranch(ListProjectsBranchDefect, sdkgo.FailureValidation, "pageIndex must be nonnegative and pageSize must be between 1 and 100")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.ClientID.Reveal()) || !isValidCredential(credentials.ClientSecret.Reveal()) {
		return projectBranch(ListProjectsBranchDefect, sdkgo.FailureAuthentication, "Worktile client credentials are unavailable or invalid")
	}
	token, attempt, hasAttempt := operation.requestTenantToken(call, credentials)
	if hasAttempt {
		return attempt
	}
	endpoint, err := url.Parse(projectsEndpoint)
	if err != nil {
		return projectBranch(ListProjectsBranchDefect, sdkgo.FailureValidation, "Worktile project endpoint is invalid")
	}
	// Worktile declares access_token only as a query parameter: https://worktile.apifox.cn/api-101771901.md
	parameters := url.Values{"access_token": {token}, "page_index": {strconv.Itoa(input.PageIndex)}, "page_size": {strconv.Itoa(input.PageSize)}}
	endpoint.RawQuery = parameters.Encode()
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return projectBranch(ListProjectsBranchDefect, sdkgo.FailureValidation, "Worktile project request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return projectRetry(sdkgo.FailureAvailability, "Worktile project-list request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return projectRetry(sdkgo.FailureRateLimit, "Worktile rate limited project listing", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return projectRetry(sdkgo.FailureAvailability, "Worktile project listing is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyProjectStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return projectBranch(ListProjectsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Worktile project response exceeded the configured limit")
	}
	if err != nil {
		return projectRetry(sdkgo.FailureAvailability, "Worktile project response was interrupted", 0)
	}
	var wire struct {
		PageIndex *int  `json:"page_index"`
		PageSize  *int  `json:"page_size"`
		HasMore   *bool `json:"has_more"`
		Items     *[]struct {
			ID         string `json:"_id"`
			Name       string `json:"name"`
			Identifier string `json:"identifier"`
			Visibility *int   `json:"visibility"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.PageIndex == nil || wire.PageSize == nil || wire.HasMore == nil || wire.Items == nil || *wire.PageIndex != input.PageIndex || *wire.PageSize < 1 || *wire.PageSize > 100 {
		return projectBranch(ListProjectsBranchInvalidResponse, sdkgo.FailureProtocol, "Worktile returned invalid project page data")
	}
	output := ListProjectsOutput{PageIndex: *wire.PageIndex, PageSize: *wire.PageSize, HasMore: *wire.HasMore, Projects: make([]Project, 0, len(*wire.Items))}
	for _, item := range *wire.Items {
		if item.ID == "" || item.Name == "" || item.Visibility == nil || (*item.Visibility != 1 && *item.Visibility != 2) {
			return projectBranch(ListProjectsBranchInvalidResponse, sdkgo.FailureProtocol, "Worktile returned invalid project metadata")
		}
		output.Projects = append(output.Projects, Project{ID: item.ID, Name: item.Name, Identifier: item.Identifier, Visibility: *item.Visibility})
	}
	return sdkgo.NewQueryBranch(ListProjectsBranchFound, output, nil, sdkgo.Receipt{})
}
func (operation ListProjectsOperation) requestTenantToken(call sdkgo.Call, credentials Credentials) (string, sdkgo.QueryAttempt[ListProjectsOutput], bool) {
	body, err := json.Marshal(struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}{credentials.ClientID.Reveal(), credentials.ClientSecret.Reveal()})
	if err != nil {
		return "", projectBranch(ListProjectsBranchDefect, sdkgo.FailureValidation, "Worktile token request is invalid"), true
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, tenantTokenEndpoint, bytes.NewReader(body))
	if err != nil {
		return "", projectBranch(ListProjectsBranchDefect, sdkgo.FailureValidation, "Worktile token request is invalid"), true
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return "", projectRetry(sdkgo.FailureAvailability, "Worktile token request failed", 0), true
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return "", projectRetry(sdkgo.FailureRateLimit, "Worktile rate limited the token request", retryAfter), true
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return "", projectRetry(sdkgo.FailureAvailability, "Worktile token service is temporarily unavailable", retryAfter), true
	}
	if response.StatusCode != http.StatusOK {
		return "", classifyProjectStatus(response.StatusCode), true
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return "", projectBranch(ListProjectsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Worktile token response exceeded the configured limit"), true
	}
	if err != nil {
		return "", projectRetry(sdkgo.FailureAvailability, "Worktile token response was interrupted", 0), true
	}
	var wire struct {
		Token     string `json:"tenant_access_token"`
		ExpiresIn *int64 `json:"expires_in"`
	}
	if err := json.Unmarshal(responseBody, &wire); err != nil || !isValidCredential(wire.Token) || wire.ExpiresIn == nil || *wire.ExpiresIn <= 0 {
		return "", projectBranch(ListProjectsBranchInvalidResponse, sdkgo.FailureProtocol, "Worktile returned invalid token data"), true
	}
	return wire.Token, sdkgo.QueryAttempt[ListProjectsOutput]{}, false
}
func classifyProjectStatus(status int) sdkgo.QueryAttempt[ListProjectsOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return projectBranch(ListProjectsBranchProviderRejected, kind, "Worktile rejected the request with HTTP "+strconv.Itoa(status))
}
func projectBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListProjectsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "worktile", Operation: "listProjects", Message: message}
	return sdkgo.NewQueryBranch(branch, ListProjectsOutput{}, &failure, sdkgo.Receipt{})
}
func projectRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListProjectsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "worktile", Operation: "listProjects", Message: message}
	return sdkgo.NewQueryRetry[ListProjectsOutput](failure, delay)
}
