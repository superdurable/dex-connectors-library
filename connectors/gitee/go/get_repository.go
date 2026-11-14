// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gitee

import (
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// GetRepositoryInput selects one Gitee repository by its URL owner and repository path segments.
type GetRepositoryInput struct {
	// Owner is the organization, enterprise, or user path from the Gitee repository URL.
	Owner string `json:"owner"`
	// Repo is the repository path from the Gitee repository URL.
	Repo string `json:"repo"`
}

// RepositoryMetadata contains stable public metadata returned by Gitee.
type RepositoryMetadata struct {
	// ID is Gitee's repository identifier.
	ID int64 `json:"id"`
	// FullName is the owner and repository path.
	FullName string `json:"fullName"`
	// Name is the repository name.
	Name string `json:"name"`
	// Description is the repository description, if any.
	Description string `json:"description"`
	// HTMLURL is the repository page URL.
	HTMLURL string `json:"htmlUrl"`
	// Private reports whether the repository is private.
	Private bool `json:"private"`
	// DefaultBranch is the default branch name.
	DefaultBranch string `json:"defaultBranch"`
	// StarsCount is the current star count.
	StarsCount int64 `json:"starsCount"`
	// ForksCount is the current fork count.
	ForksCount int64 `json:"forksCount"`
}

// GetRepositoryOutput contains the requested repository metadata.
type GetRepositoryOutput struct {
	Repository RepositoryMetadata `json:"repository"`
}

// GetRepositoryOperation reads one repository.
type GetRepositoryOperation struct{ client *Client }

// Definition returns the generated repository contract.
func (GetRepositoryOperation) Definition() sdkgo.QueryDefinition { return GetRepositoryDefinition }

// Invoke reads repository metadata and classifies Gitee and transport outcomes.
func (operation GetRepositoryOperation) Invoke(call sdkgo.Call, input GetRepositoryInput) sdkgo.QueryAttempt[GetRepositoryOutput] {
	if !isValidPathSegment(input.Owner) || !isValidPathSegment(input.Repo) {
		return repositoryBranch(GetRepositoryBranchDefect, sdkgo.FailureValidation, "owner or repo path is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidToken(credentials.AccessToken.Reveal()) {
		return repositoryBranch(GetRepositoryBranchDefect, sdkgo.FailureAuthentication, "connection token is unavailable or invalid")
	}
	endpoint := operation.client.baseURL + "/api/v5/repos/" + url.PathEscape(input.Owner) + "/" + url.PathEscape(input.Repo)
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return repositoryBranch(GetRepositoryBranchDefect, sdkgo.FailureValidation, "repository request is invalid")
	}
	query := request.URL.Query()
	// Gitee v5 declares access_token only as a query parameter here: https://gitee.com/api/v5/swagger
	query.Set("access_token", credentials.AccessToken.Reveal())
	request.URL.RawQuery = query.Encode()
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return repositoryRetry(sdkgo.FailureAvailability, "Gitee request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return repositoryRetry(sdkgo.FailureRateLimit, "Gitee rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return repositoryRetry(sdkgo.FailureAvailability, "Gitee is temporarily unavailable", retryAfter)
	}
	if response.StatusCode == http.StatusNotFound {
		return repositoryBranch(GetRepositoryBranchNotFound, sdkgo.FailureProviderRejection, "repository was not found or is not visible")
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return repositoryBranch(GetRepositoryBranchProviderRejected, kind, "Gitee rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return repositoryBranch(GetRepositoryBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Gitee response exceeded the configured limit")
	}
	if err != nil {
		return repositoryRetry(sdkgo.FailureAvailability, "Gitee response was interrupted", 0)
	}
	var wire struct {
		ID            int64  `json:"id"`
		FullName      string `json:"full_name"`
		Name          string `json:"name"`
		Description   string `json:"description"`
		HTMLURL       string `json:"html_url"`
		Private       bool   `json:"private"`
		DefaultBranch string `json:"default_branch"`
		StarsCount    int64  `json:"stargazers_count"`
		ForksCount    int64  `json:"forks_count"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.ID <= 0 || wire.FullName == "" || wire.Name == "" || wire.StarsCount < 0 || wire.ForksCount < 0 {
		return repositoryBranch(GetRepositoryBranchInvalidResponse, sdkgo.FailureProtocol, "Gitee returned invalid repository metadata")
	}
	return sdkgo.NewQueryBranch(GetRepositoryBranchFound, GetRepositoryOutput{Repository: RepositoryMetadata{ID: wire.ID, FullName: wire.FullName, Name: wire.Name, Description: wire.Description, HTMLURL: wire.HTMLURL, Private: wire.Private, DefaultBranch: wire.DefaultBranch, StarsCount: wire.StarsCount, ForksCount: wire.ForksCount}}, nil, sdkgo.Receipt{})
}
func isValidPathSegment(segment string) bool {
	if segment == "" || len(segment) > 128 || strings.TrimSpace(segment) != segment {
		return false
	}
	for _, character := range segment {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.') {
			return false
		}
	}
	return segment != "." && segment != ".."
}
func repositoryBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetRepositoryOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "gitee", Operation: "getRepository", Message: message}
	return sdkgo.NewQueryBranch(branch, GetRepositoryOutput{}, &failure, sdkgo.Receipt{})
}
func repositoryRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetRepositoryOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "gitee", Operation: "getRepository", Message: message}
	return sdkgo.NewQueryRetry[GetRepositoryOutput](failure, delay)
}
