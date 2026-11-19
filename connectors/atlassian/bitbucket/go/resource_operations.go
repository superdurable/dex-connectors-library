// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bitbucket

import (
	"bytes"
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

// GetRepositoryInput identifies one repository within a workspace.
type GetRepositoryInput struct {
	// Workspace is the Bitbucket workspace slug.
	Workspace string `json:"workspace"`
	// Repository is the repository slug in that workspace.
	Repository string `json:"repository"`
}

// GetRepositoryOperation reads one repository.
type GetRepositoryOperation struct{ client *Client }

// GetRepository binds the Query to this client.
func (client *Client) GetRepository() GetRepositoryOperation { return GetRepositoryOperation{client} }

// Definition returns the manifest-generated Query definition.
func (GetRepositoryOperation) Definition() sdkgo.QueryDefinition { return GetRepositoryDefinition }

// Invoke reads the repository and classifies provider outcomes.
func (operation GetRepositoryOperation) Invoke(call sdkgo.Call, input GetRepositoryInput) sdkgo.QueryAttempt[Repository] {
	if !validPathSegment(input.Workspace) || !validPathSegment(input.Repository) {
		return failedAttempt[Repository]("getRepository", GetRepositoryBranchDefect, sdkgo.FailureValidation, "workspace and repository must be path segments")
	}
	path := "/repositories/" + url.PathEscape(input.Workspace) + "/" + url.PathEscape(input.Repository)
	return executeRead(operation.client, call, "getRepository", path, nil, GetRepositoryBranchFound, GetRepositoryBranchNotFound, GetRepositoryBranchProviderRejected, GetRepositoryBranchInvalidResponse, GetRepositoryBranchDefect, decodeRepository)
}

func decodeRepository(body []byte) (Repository, error) {
	var repository Repository
	if err := json.Unmarshal(body, &repository); err != nil {
		return Repository{}, err
	}
	if repository.UUID == "" || repository.Name == "" || repository.FullName == "" {
		return Repository{}, errors.New("repository identity is incomplete")
	}
	return repository, nil
}

// GetPullRequestInput identifies one repository-local pull request.
type GetPullRequestInput struct {
	// Workspace is the Bitbucket workspace slug.
	Workspace string `json:"workspace"`
	// Repository is the repository slug in that workspace.
	Repository string `json:"repository"`
	// PullRequestID is the positive numeric ID in that repository.
	PullRequestID int64 `json:"pullRequestId"`
}

// GetPullRequestOperation reads one pull request.
type GetPullRequestOperation struct{ client *Client }

// GetPullRequest binds the Query to this client.
func (client *Client) GetPullRequest() GetPullRequestOperation {
	return GetPullRequestOperation{client}
}

// Definition returns the manifest-generated Query definition.
func (GetPullRequestOperation) Definition() sdkgo.QueryDefinition { return GetPullRequestDefinition }

// Invoke reads the pull request and classifies provider outcomes.
func (operation GetPullRequestOperation) Invoke(call sdkgo.Call, input GetPullRequestInput) sdkgo.QueryAttempt[PullRequest] {
	if !validPathSegment(input.Workspace) || !validPathSegment(input.Repository) || input.PullRequestID <= 0 {
		return failedAttempt[PullRequest]("getPullRequest", GetPullRequestBranchDefect, sdkgo.FailureValidation, "workspace, repository, or pull request ID is invalid")
	}
	path := pullRequestPath(input.Workspace, input.Repository, input.PullRequestID)
	return executeRead(operation.client, call, "getPullRequest", path, nil, GetPullRequestBranchFound, GetPullRequestBranchNotFound, GetPullRequestBranchProviderRejected, GetPullRequestBranchInvalidResponse, GetPullRequestBranchDefect, decodePullRequest)
}

func decodePullRequest(body []byte) (PullRequest, error) {
	var pullRequest PullRequest
	if err := json.Unmarshal(body, &pullRequest); err != nil {
		return PullRequest{}, err
	}
	if pullRequest.ID <= 0 || pullRequest.Title == "" || pullRequest.State == "" {
		return PullRequest{}, errors.New("pull request identity is incomplete")
	}
	return pullRequest, nil
}

// UpdatePullRequestTitleInput selects a pull request and supplies its complete new title.
type UpdatePullRequestTitleInput struct {
	// Workspace is the Bitbucket workspace slug.
	Workspace string `json:"workspace"`
	// Repository is the repository slug in that workspace.
	Repository string `json:"repository"`
	// PullRequestID is the positive numeric ID in that repository.
	PullRequestID int64 `json:"pullRequestId"`
	// Title is the full replacement title; blank is invalid.
	Title string `json:"title"`
}

// UpdatePullRequestTitleOperation replaces a pull request title.
type UpdatePullRequestTitleOperation struct{ client *Client }

// UpdatePullRequestTitle binds the Mutation to this client.
func (client *Client) UpdatePullRequestTitle() UpdatePullRequestTitleOperation {
	return UpdatePullRequestTitleOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (UpdatePullRequestTitleOperation) Definition() sdkgo.MutationDefinition {
	return UpdatePullRequestTitleDefinition
}

// IdempotencyKey derives a stable receipt identity; title replacement is safe without provider key support.
func (UpdatePullRequestTitleOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdatePullRequestTitleInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends an absolute title replacement and classifies provider outcomes.
func (operation UpdatePullRequestTitleOperation) Invoke(call sdkgo.Call, input UpdatePullRequestTitleInput) sdkgo.MutationAttempt[PullRequest] {
	const name = "updatePullRequestTitle"
	if !validPathSegment(input.Workspace) || !validPathSegment(input.Repository) || input.PullRequestID <= 0 || strings.TrimSpace(input.Title) == "" || len(input.Title) > 512 {
		return updateTitleBranch(UpdatePullRequestTitleBranchDefect, sdkgo.FailureValidation, "workspace, repository, pull request ID, or title is invalid")
	}
	payload, err := json.Marshal(struct {
		Title string `json:"title"`
	}{Title: input.Title})
	if err != nil {
		return updateTitleBranch(UpdatePullRequestTitleBranchDefect, sdkgo.FailureLocalDefect, "pull request title could not be encoded")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPut, operation.client.baseURL+pullRequestPath(input.Workspace, input.Repository, input.PullRequestID), bytes.NewReader(payload))
	if err != nil {
		return updateTitleBranch(UpdatePullRequestTitleBranchDefect, sdkgo.FailureLocalDefect, "pull request update could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	authorization := operation.client.authorize(call, name, request, UpdatePullRequestTitleBranchDefect, UpdatePullRequestTitleBranchProviderRejected, UpdatePullRequestTitleBranchInvalidResponse)
	if authorization.failure != nil {
		return sdkgo.NewMutationBranch(authorization.branch, PullRequest{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[PullRequest](*failureFor(name, sdkgo.FailureTransport, "Bitbucket pull request update failed"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[PullRequest](*failureFor(name, sdkgo.FailureRateLimit, "Bitbucket rate limited the update"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewMutationRetry[PullRequest](*failureFor(name, sdkgo.FailureAvailability, "Bitbucket pull request update is unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusNotFound:
		return updateTitleBranch(UpdatePullRequestTitleBranchNotFound, sdkgo.FailureNotFound, "pull request was not found")
	case response.StatusCode == http.StatusUnauthorized:
		return updateTitleBranch(UpdatePullRequestTitleBranchProviderRejected, sdkgo.FailureAuthentication, "Bitbucket rejected credentials")
	case response.StatusCode == http.StatusForbidden:
		return updateTitleBranch(UpdatePullRequestTitleBranchProviderRejected, sdkgo.FailureAuthorization, "Bitbucket denied the title update")
	case response.StatusCode != http.StatusOK:
		return updateTitleBranch(UpdatePullRequestTitleBranchProviderRejected, sdkgo.FailureProviderRejection, "Bitbucket rejected title update with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return updateTitleBranch(UpdatePullRequestTitleBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Bitbucket response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[PullRequest](*failureFor(name, sdkgo.FailureTransport, "Bitbucket update response was interrupted"), 0)
	}
	pullRequest, err := decodePullRequest(body)
	if err != nil || pullRequest.ID != input.PullRequestID || pullRequest.Title != input.Title {
		return updateTitleBranch(UpdatePullRequestTitleBranchInvalidResponse, sdkgo.FailureProtocol, "Bitbucket returned a different pull request or title")
	}
	return sdkgo.NewMutationBranch(UpdatePullRequestTitleBranchUpdated, pullRequest, nil, sdkgo.Receipt{})
}

func updateTitleBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[PullRequest] {
	return sdkgo.NewMutationBranch(branch, PullRequest{}, failureFor("updatePullRequestTitle", kind, message), sdkgo.Receipt{})
}

func pullRequestPath(workspace, repository string, pullRequestID int64) string {
	return "/repositories/" + url.PathEscape(workspace) + "/" + url.PathEscape(repository) + "/pullrequests/" + strconv.FormatInt(pullRequestID, 10)
}
