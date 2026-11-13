// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gitcode

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetRepositoryInput identifies a repository by its GitCode URL path components.
type GetRepositoryInput struct {
	// Owner is the organization or user path before the slash.
	Owner string `json:"owner"`
	// Repo is the repository path after the slash.
	Repo string `json:"repo"`
}

// GetRepositoryOutput includes only repository identity and visibility, omitting members and creator details.
type GetRepositoryOutput struct {
	// ID is the GitCode repository ID.
	ID int64 `json:"id"`
	// FullName is the owner/repository path.
	FullName string `json:"fullName"`
	// Private indicates whether this repository is private.
	Private bool `json:"private"`
	// DefaultBranch is the repository's default branch, if configured.
	DefaultBranch string `json:"defaultBranch"`
}

// GetRepositoryOperation reads one repository's details.
type GetRepositoryOperation struct{ client *Client }

// Definition returns the generated repository Query contract.
func (GetRepositoryOperation) Definition() sdkgo.QueryDefinition { return GetRepositoryDefinition }

// Invoke sends one authenticated read-only GET and classifies provider outcomes.
func (operation GetRepositoryOperation) Invoke(call sdkgo.Call, input GetRepositoryInput) sdkgo.QueryAttempt[GetRepositoryOutput] {
	if !isValidPathSegment(input.Owner) || !isValidPathSegment(input.Repo) {
		return repositoryBranch(GetRepositoryBranchDefect, sdkgo.FailureValidation, "owner or repository path is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAccessToken(credentials.AccessToken.Reveal()) {
		return repositoryBranch(GetRepositoryBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/api/v5/repos/"+input.Owner+"/"+input.Repo, nil)
	if err != nil {
		return repositoryBranch(GetRepositoryBranchDefect, sdkgo.FailureValidation, "repository request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return repositoryRetry(sdkgo.FailureAvailability, "GitCode request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return repositoryRetry(sdkgo.FailureRateLimit, "GitCode rate limit reached", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return repositoryRetry(sdkgo.FailureAvailability, "GitCode is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return repositoryBranch(GetRepositoryBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "GitCode response exceeded the configured limit")
	}
	if err != nil {
		return repositoryRetry(sdkgo.FailureAvailability, "GitCode response was interrupted", 0)
	}
	if response.StatusCode == http.StatusNotFound {
		return repositoryBranch(GetRepositoryBranchNotFound, sdkgo.FailureProviderRejection, "GitCode repository was not found or is inaccessible")
	}
	if response.StatusCode != http.StatusOK {
		return repositoryBranch(GetRepositoryBranchProviderRejected, classifyGitCodeStatus(response.StatusCode), "GitCode rejected the repository lookup")
	}
	var wire struct {
		ID            int64  `json:"id"`
		FullName      string `json:"full_name"`
		Private       *bool  `json:"private"`
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.ID <= 0 || wire.FullName == "" || wire.Private == nil {
		return repositoryBranch(GetRepositoryBranchInvalidResponse, sdkgo.FailureProtocol, "GitCode returned an invalid repository")
	}
	return sdkgo.NewQueryBranch(GetRepositoryBranchFetched, GetRepositoryOutput{ID: wire.ID, FullName: wire.FullName, Private: *wire.Private, DefaultBranch: wire.DefaultBranch}, nil, sdkgo.Receipt{})
}

func isValidPathSegment(segment string) bool {
	if segment == "" || len(segment) > 128 || segment == "." || segment == ".." {
		return false
	}
	for _, character := range segment {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '_' || character == '-' || character == '.') {
			return false
		}
	}
	return true
}
func isValidAccessToken(token string) bool {
	if token == "" || len(token) > 512 || strings.TrimSpace(token) != token {
		return false
	}
	for _, character := range token {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func classifyGitCodeStatus(statusCode int) sdkgo.FailureKind {
	switch statusCode {
	case http.StatusUnauthorized:
		return sdkgo.FailureAuthentication
	case http.StatusBadRequest:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func repositoryBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetRepositoryOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "gitcode", Operation: "getRepository", Message: message}
	return sdkgo.NewQueryBranch(branch, GetRepositoryOutput{}, &failure, sdkgo.Receipt{})
}
func repositoryRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetRepositoryOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "gitcode", Operation: "getRepository", Message: message}
	return sdkgo.NewQueryRetry[GetRepositoryOutput](failure, delay)
}
