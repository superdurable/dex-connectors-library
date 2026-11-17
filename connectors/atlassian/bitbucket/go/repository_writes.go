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

// CreateRepositoryInput selects a stable slug, name, and visibility.
type CreateRepositoryInput struct {
	// Workspace is the owning Bitbucket workspace slug.
	Workspace string `json:"workspace"`
	// Repository is the new repository slug.
	Repository string `json:"repository"`
	// Name is the display name, up to 255 bytes.
	Name string `json:"name"`
	// IsPrivate selects private or public visibility; it must be supplied.
	IsPrivate *bool `json:"is_private"`
}

// CreateRepositoryOperation creates a repository at a stable workspace/slug path.
type CreateRepositoryOperation struct{ client *Client }

// CreateRepository binds the Mutation to this client.
func (client *Client) CreateRepository() CreateRepositoryOperation {
	return CreateRepositoryOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (CreateRepositoryOperation) Definition() sdkgo.MutationDefinition {
	return CreateRepositoryDefinition
}

// IdempotencyKey records the Dex call identity; the path itself prevents duplicate repositories.
func (CreateRepositoryOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateRepositoryInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates or confirms the repository at its chosen stable path.
func (operation CreateRepositoryOperation) Invoke(call sdkgo.Call, input CreateRepositoryInput) sdkgo.MutationAttempt[Repository] {
	if !validPathSegment(input.Workspace) || !validPathSegment(input.Repository) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 || input.IsPrivate == nil {
		return repositoryBranch("createRepository", CreateRepositoryBranchDefect, sdkgo.FailureValidation, "workspace, repository, name, or visibility is invalid")
	}
	path := repositoryPath(input.Workspace, input.Repository)
	payload := struct {
		SCM       string `json:"scm"`
		Name      string `json:"name"`
		IsPrivate bool   `json:"is_private"`
	}{SCM: "git", Name: input.Name, IsPrivate: *input.IsPrivate}
	return executeRepositoryWrite(operation.client, call, "createRepository", http.MethodPost, path, payload, CreateRepositoryBranchCreated, CreateRepositoryBranchNotFound, CreateRepositoryBranchProviderRejected, CreateRepositoryBranchInvalidResponse, CreateRepositoryBranchDefect, func(repository Repository) bool {
		return repository.FullName == input.Workspace+"/"+input.Repository && repository.Name == input.Name && repository.IsPrivate == *input.IsPrivate
	}, true)
}

// UpdateRepositoryInput replaces a repository name and visibility.
type UpdateRepositoryInput struct {
	// Workspace is the owning Bitbucket workspace slug.
	Workspace string `json:"workspace"`
	// Repository is the repository slug.
	Repository string `json:"repository"`
	// Name is the complete replacement display name, up to 255 bytes.
	Name string `json:"name"`
	// IsPrivate selects private or public visibility; it must be supplied.
	IsPrivate *bool `json:"is_private"`
}

// UpdateRepositoryOperation replaces a repository name and visibility.
type UpdateRepositoryOperation struct{ client *Client }

// UpdateRepository binds the Mutation to this client.
func (client *Client) UpdateRepository() UpdateRepositoryOperation {
	return UpdateRepositoryOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (UpdateRepositoryOperation) Definition() sdkgo.MutationDefinition {
	return UpdateRepositoryDefinition
}

// IdempotencyKey records the stable Dex call identity.
func (UpdateRepositoryOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateRepositoryInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces name and visibility with absolute values.
func (operation UpdateRepositoryOperation) Invoke(call sdkgo.Call, input UpdateRepositoryInput) sdkgo.MutationAttempt[Repository] {
	if !validPathSegment(input.Workspace) || !validPathSegment(input.Repository) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 || input.IsPrivate == nil {
		return repositoryBranch("updateRepository", UpdateRepositoryBranchDefect, sdkgo.FailureValidation, "workspace, repository, name, or visibility is invalid")
	}
	payload := struct {
		Name      string `json:"name"`
		IsPrivate bool   `json:"is_private"`
	}{Name: input.Name, IsPrivate: *input.IsPrivate}
	return executeRepositoryWrite(operation.client, call, "updateRepository", http.MethodPut, repositoryPath(input.Workspace, input.Repository), payload, UpdateRepositoryBranchUpdated, UpdateRepositoryBranchNotFound, UpdateRepositoryBranchProviderRejected, UpdateRepositoryBranchInvalidResponse, UpdateRepositoryBranchDefect, func(repository Repository) bool {
		return repository.FullName == input.Workspace+"/"+input.Repository && repository.Name == input.Name && repository.IsPrivate == *input.IsPrivate
	}, false)
}

// DeleteRepositoryInput identifies a repository to delete.
type DeleteRepositoryInput struct {
	// Workspace is the owning Bitbucket workspace slug.
	Workspace string `json:"workspace"`
	// Repository is the repository slug.
	Repository string `json:"repository"`
}

// DeletedRepository records the workspace and slug whose deletion succeeded.
type DeletedRepository struct {
	// Workspace is the owning Bitbucket workspace slug.
	Workspace string `json:"workspace"`
	// Repository is the deleted repository slug.
	Repository string `json:"repository"`
}

// DeleteRepositoryOperation removes a repository.
type DeleteRepositoryOperation struct{ client *Client }

// DeleteRepository binds the Mutation to this client.
func (client *Client) DeleteRepository() DeleteRepositoryOperation {
	return DeleteRepositoryOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (DeleteRepositoryOperation) Definition() sdkgo.MutationDefinition {
	return DeleteRepositoryDefinition
}

// IdempotencyKey records the stable Dex call identity.
func (DeleteRepositoryOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteRepositoryInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the selected repository.
func (operation DeleteRepositoryOperation) Invoke(call sdkgo.Call, input DeleteRepositoryInput) sdkgo.MutationAttempt[DeletedRepository] {
	const name = "deleteRepository"
	if !validPathSegment(input.Workspace) || !validPathSegment(input.Repository) {
		return sdkgo.NewMutationBranch(DeleteRepositoryBranchDefect, DeletedRepository{}, failureFor(name, sdkgo.FailureValidation, "workspace or repository is invalid"), sdkgo.Receipt{})
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodDelete, operation.client.baseURL+repositoryPath(input.Workspace, input.Repository), nil)
	if err != nil {
		return sdkgo.NewMutationBranch(DeleteRepositoryBranchDefect, DeletedRepository{}, failureFor(name, sdkgo.FailureLocalDefect, "repository delete request could not be built"), sdkgo.Receipt{})
	}
	request.Header.Set("Accept", "application/json")
	authorization := operation.client.authorize(call, name, request, DeleteRepositoryBranchDefect, DeleteRepositoryBranchProviderRejected, DeleteRepositoryBranchInvalidResponse)
	if authorization.failure != nil {
		return sdkgo.NewMutationBranch(authorization.branch, DeletedRepository{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[DeletedRepository](*failureFor(name, sdkgo.FailureTransport, "repository delete request failed"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[DeletedRepository](*failureFor(name, sdkgo.FailureRateLimit, "Bitbucket rate limited deletion"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewMutationRetry[DeletedRepository](*failureFor(name, sdkgo.FailureAvailability, "repository deletion is unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusNotFound:
		return sdkgo.NewMutationBranch(DeleteRepositoryBranchNotFound, DeletedRepository{}, failureFor(name, sdkgo.FailureNotFound, "repository was not found"), sdkgo.Receipt{})
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return sdkgo.NewMutationBranch(DeleteRepositoryBranchProviderRejected, DeletedRepository{}, failureFor(name, sdkgo.FailureAuthorization, "Bitbucket denied deletion"), sdkgo.Receipt{})
	case response.StatusCode == http.StatusOK:
		return sdkgo.NewMutationBranch(DeleteRepositoryBranchInvalidResponse, DeletedRepository{}, failureFor(name, sdkgo.FailureProtocol, "Bitbucket returned an unexpected deletion response"), sdkgo.Receipt{})
	case response.StatusCode != http.StatusNoContent:
		return sdkgo.NewMutationBranch(DeleteRepositoryBranchProviderRejected, DeletedRepository{}, failureFor(name, sdkgo.FailureProviderRejection, "Bitbucket rejected deletion with HTTP "+strconv.Itoa(response.StatusCode)), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(DeleteRepositoryBranchDeleted, DeletedRepository{Workspace: input.Workspace, Repository: input.Repository}, nil, sdkgo.Receipt{})
}

func executeRepositoryWrite(client *Client, call sdkgo.Call, name, method, path string, payload any, successBranch, notFoundBranch, rejectedBranch, invalidBranch, defectBranch sdkgo.BranchID, matches func(Repository) bool, reconcileConflict bool) sdkgo.MutationAttempt[Repository] {
	body, err := json.Marshal(payload)
	if err != nil {
		return repositoryBranch(name, defectBranch, sdkgo.FailureLocalDefect, "repository request could not be encoded")
	}
	request, err := http.NewRequestWithContext(call.Context, method, client.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return repositoryBranch(name, defectBranch, sdkgo.FailureLocalDefect, "repository request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	authorization := client.authorize(call, name, request, defectBranch, rejectedBranch, invalidBranch)
	if authorization.failure != nil {
		return sdkgo.NewMutationBranch(authorization.branch, Repository{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[Repository](*failureFor(name, sdkgo.FailureTransport, "repository write request failed"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[Repository](*failureFor(name, sdkgo.FailureRateLimit, "Bitbucket rate limited repository write"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewMutationRetry[Repository](*failureFor(name, sdkgo.FailureAvailability, "repository write is unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case reconcileConflict && response.StatusCode == http.StatusConflict:
		return client.reconcileRepositoryConflict(call, name, path, successBranch, rejectedBranch, invalidBranch, matches)
	case response.StatusCode == http.StatusNotFound:
		return repositoryBranch(name, notFoundBranch, sdkgo.FailureNotFound, "repository was not found")
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return repositoryBranch(name, rejectedBranch, sdkgo.FailureAuthorization, "Bitbucket denied repository write")
	case response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated:
		return repositoryBranch(name, rejectedBranch, sdkgo.FailureProviderRejection, "Bitbucket rejected repository write with HTTP "+strconv.Itoa(response.StatusCode))
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return repositoryBranch(name, invalidBranch, sdkgo.FailureResponseTooLarge, "repository response exceeded configured limit")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[Repository](*failureFor(name, sdkgo.FailureTransport, "repository response was interrupted"), 0)
	}
	repository, err := decodeRepository(responseBody)
	if err != nil || !matches(repository) {
		return repositoryBranch(name, invalidBranch, sdkgo.FailureProtocol, "Bitbucket returned a different repository state")
	}
	return sdkgo.NewMutationBranch(successBranch, repository, nil, sdkgo.Receipt{})
}

func (client *Client) reconcileRepositoryConflict(call sdkgo.Call, name, path string, successBranch, rejectedBranch, invalidBranch sdkgo.BranchID, matches func(Repository) bool) sdkgo.MutationAttempt[Repository] {
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return repositoryBranch(name, invalidBranch, sdkgo.FailureLocalDefect, "repository readback could not be built")
	}
	request.Header.Set("Accept", "application/json")
	authorization := client.authorize(call, name, request, invalidBranch, rejectedBranch, invalidBranch)
	if authorization.failure != nil {
		return sdkgo.NewMutationBranch(authorization.branch, Repository{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[Repository](*failureFor(name, sdkgo.FailureTransport, "repository readback failed"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return repositoryBranch(name, rejectedBranch, sdkgo.FailureProviderRejection, "repository exists but could not be confirmed")
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return repositoryBranch(name, invalidBranch, sdkgo.FailureProtocol, "repository readback response is invalid")
	}
	repository, err := decodeRepository(body)
	if err != nil || !matches(repository) {
		return repositoryBranch(name, rejectedBranch, sdkgo.FailureProviderRejection, "repository slug belongs to a different resource")
	}
	return sdkgo.NewMutationBranch(successBranch, repository, nil, sdkgo.Receipt{})
}

func repositoryPath(workspace, repository string) string {
	return "/repositories/" + url.PathEscape(workspace) + "/" + url.PathEscape(repository)
}

func repositoryBranch(name string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[Repository] {
	return sdkgo.NewMutationBranch(branch, Repository{}, failureFor(name, kind, message), sdkgo.Receipt{})
}
