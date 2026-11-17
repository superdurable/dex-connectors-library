// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bitbucket

import (
	"bytes"
	"encoding/hex"
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

// BranchTarget identifies the commit at a Bitbucket branch tip.
type BranchTarget struct {
	// Hash is the full target commit hash.
	Hash string `json:"hash"`
}

// Branch is a Bitbucket repository branch.
type Branch struct {
	// Name is the branch name without refs/heads/.
	Name string `json:"name"`
	// Target identifies the branch tip.
	Target BranchTarget `json:"target"`
}

// BranchPage is one provider page of branches.
type BranchPage struct {
	// Values contains the returned branches.
	Values []Branch `json:"values"`
	// Next is an untrusted provider pagination URL.
	Next string `json:"next"`
	// Page is the one-based page number.
	Page int `json:"page"`
	// PageLength is the provider page length.
	PageLength int `json:"pagelen"`
}

// ListBranchesInput identifies a Bitbucket repository.
type ListBranchesInput struct {
	// Workspace is the Bitbucket workspace slug.
	Workspace string `json:"workspace"`
	// Repository is the repository slug.
	Repository string `json:"repository"`
}

// BranchRefInput identifies one branch in a repository.
type BranchRefInput struct {
	// Workspace is the Bitbucket workspace slug.
	Workspace string `json:"workspace"`
	// Repository is the repository slug.
	Repository string `json:"repository"`
	// Name is the branch name without refs/heads/.
	Name string `json:"name"`
}

// ListBranchesOperation reads one page of branches.
type ListBranchesOperation struct{ client *Client }

// ListBranches binds the Query to this client.
func (client *Client) ListBranches() ListBranchesOperation { return ListBranchesOperation{client} }

// Definition returns the manifest-generated Query definition.
func (ListBranchesOperation) Definition() sdkgo.QueryDefinition { return ListBranchesDefinition }

// Invoke lists branches in one repository.
func (operation ListBranchesOperation) Invoke(call sdkgo.Call, input ListBranchesInput) sdkgo.QueryAttempt[BranchPage] {
	if !validPathSegment(input.Workspace) || !validPathSegment(input.Repository) {
		return failedAttempt[BranchPage]("listBranches", ListBranchesBranchDefect, sdkgo.FailureValidation, "workspace or repository is invalid")
	}
	return executeRead(operation.client, call, "listBranches", branchesPath(input.Workspace, input.Repository), nil, ListBranchesBranchListed, ListBranchesBranchNotFound, ListBranchesBranchProviderRejected, ListBranchesBranchInvalidResponse, ListBranchesBranchDefect, decodeBranchPage)
}

// GetBranchOperation reads one branch.
type GetBranchOperation struct{ client *Client }

// GetBranch binds the Query to this client.
func (client *Client) GetBranch() GetBranchOperation { return GetBranchOperation{client} }

// Definition returns the manifest-generated Query definition.
func (GetBranchOperation) Definition() sdkgo.QueryDefinition { return GetBranchDefinition }

// Invoke reads a branch by name.
func (operation GetBranchOperation) Invoke(call sdkgo.Call, input BranchRefInput) sdkgo.QueryAttempt[Branch] {
	if !validBranchRefInput(input) {
		return failedAttempt[Branch]("getBranch", GetBranchBranchDefect, sdkgo.FailureValidation, "workspace, repository, or branch name is invalid")
	}
	return executeRead(operation.client, call, "getBranch", branchPath(input), nil, GetBranchBranchFound, GetBranchBranchNotFound, GetBranchBranchProviderRejected, GetBranchBranchInvalidResponse, GetBranchBranchDefect, decodeBranch)
}

// CreateBranchInput selects a name and exact target commit.
type CreateBranchInput struct {
	// Workspace is the Bitbucket workspace slug.
	Workspace string `json:"workspace"`
	// Repository is the repository slug.
	Repository string `json:"repository"`
	// Name is the new branch name without refs/heads/.
	Name string `json:"name"`
	// TargetHash is the full 40-character target commit hash.
	TargetHash string `json:"targetHash"`
}

// CreateBranchOperation creates a branch at a fixed target commit.
type CreateBranchOperation struct{ client *Client }

// CreateBranch binds the Mutation to this client.
func (client *Client) CreateBranch() CreateBranchOperation { return CreateBranchOperation{client} }

// Definition returns the manifest-generated Mutation definition.
func (CreateBranchOperation) Definition() sdkgo.MutationDefinition { return CreateBranchDefinition }

// IdempotencyKey records the Dex call identity; the branch name prevents duplicate creation.
func (CreateBranchOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateBranchInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a branch and confirms an exact matching conflict by reading it back.
func (operation CreateBranchOperation) Invoke(call sdkgo.Call, input CreateBranchInput) sdkgo.MutationAttempt[Branch] {
	const name = "createBranch"
	ref := BranchRefInput{Workspace: input.Workspace, Repository: input.Repository, Name: input.Name}
	if !validBranchRefInput(ref) || !validCommitHash(input.TargetHash) {
		return branchWriteBranch(name, CreateBranchBranchDefect, sdkgo.FailureValidation, "branch identity or full target commit hash is invalid")
	}
	payload, err := json.Marshal(struct {
		Name   string       `json:"name"`
		Target BranchTarget `json:"target"`
	}{Name: input.Name, Target: BranchTarget{Hash: input.TargetHash}})
	if err != nil {
		return branchWriteBranch(name, CreateBranchBranchDefect, sdkgo.FailureLocalDefect, "branch request could not be encoded")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+branchesPath(input.Workspace, input.Repository), bytes.NewReader(payload))
	if err != nil {
		return branchWriteBranch(name, CreateBranchBranchDefect, sdkgo.FailureLocalDefect, "branch request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	authorization := operation.client.authorize(call, name, request, CreateBranchBranchDefect, CreateBranchBranchProviderRejected, CreateBranchBranchInvalidResponse)
	if authorization.failure != nil {
		return sdkgo.NewMutationBranch(authorization.branch, Branch{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[Branch](*failureFor(name, sdkgo.FailureTransport, "branch create request failed"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[Branch](*failureFor(name, sdkgo.FailureRateLimit, "Bitbucket rate limited branch creation"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewMutationRetry[Branch](*failureFor(name, sdkgo.FailureAvailability, "branch creation is unavailable"), 0)
	case response.StatusCode == http.StatusConflict:
		return operation.client.confirmBranchConflict(call, name, branchPath(ref), CreateBranchBranchCreated, CreateBranchBranchProviderRejected, CreateBranchBranchInvalidResponse, func(branch Branch) bool {
			return branch.Name == input.Name && strings.EqualFold(branch.Target.Hash, input.TargetHash)
		})
	case response.StatusCode == http.StatusNotFound:
		return branchWriteBranch(name, CreateBranchBranchNotFound, sdkgo.FailureNotFound, "repository or target commit was not found")
	case response.StatusCode != http.StatusCreated:
		return branchWriteBranch(name, CreateBranchBranchProviderRejected, sdkgo.FailureProviderRejection, "Bitbucket rejected branch creation with HTTP "+strconv.Itoa(response.StatusCode))
	}
	branch, err := decodeBranchResponse(response, operation.client.maxResponseBytes)
	if err != nil || branch.Name != input.Name || !strings.EqualFold(branch.Target.Hash, input.TargetHash) {
		return branchWriteBranch(name, CreateBranchBranchInvalidResponse, sdkgo.FailureProtocol, "Bitbucket returned a different branch or target")
	}
	return sdkgo.NewMutationBranch(CreateBranchBranchCreated, branch, nil, sdkgo.Receipt{})
}

// DeletedBranch identifies a deleted branch.
type DeletedBranch struct {
	// Workspace is the Bitbucket workspace slug.
	Workspace string `json:"workspace"`
	// Repository is the repository slug.
	Repository string `json:"repository"`
	// Name is the deleted branch name.
	Name string `json:"name"`
}

// DeleteBranchOperation removes one branch.
type DeleteBranchOperation struct{ client *Client }

// DeleteBranch binds the Mutation to this client.
func (client *Client) DeleteBranch() DeleteBranchOperation { return DeleteBranchOperation{client} }

// Definition returns the manifest-generated Mutation definition.
func (DeleteBranchOperation) Definition() sdkgo.MutationDefinition { return DeleteBranchDefinition }

// IdempotencyKey records the Dex call identity for an absolute delete.
func (DeleteBranchOperation) IdempotencyKey(callID sdkgo.CallID, _ BranchRefInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes one branch by name.
func (operation DeleteBranchOperation) Invoke(call sdkgo.Call, input BranchRefInput) sdkgo.MutationAttempt[DeletedBranch] {
	const name = "deleteBranch"
	if !validBranchRefInput(input) {
		return deletedBranchBranch(DeleteBranchBranchDefect, sdkgo.FailureValidation, "branch identity is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodDelete, operation.client.baseURL+branchPath(input), nil)
	if err != nil {
		return deletedBranchBranch(DeleteBranchBranchDefect, sdkgo.FailureLocalDefect, "branch delete request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	authorization := operation.client.authorize(call, name, request, DeleteBranchBranchDefect, DeleteBranchBranchProviderRejected, DeleteBranchBranchDefect)
	if authorization.failure != nil {
		return sdkgo.NewMutationBranch(authorization.branch, DeletedBranch{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[DeletedBranch](*failureFor(name, sdkgo.FailureTransport, "branch delete request failed"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[DeletedBranch](*failureFor(name, sdkgo.FailureRateLimit, "Bitbucket rate limited branch deletion"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewMutationRetry[DeletedBranch](*failureFor(name, sdkgo.FailureAvailability, "branch deletion is unavailable"), 0)
	case response.StatusCode == http.StatusNotFound:
		return deletedBranchBranch(DeleteBranchBranchNotFound, sdkgo.FailureNotFound, "branch was not found")
	case response.StatusCode != http.StatusNoContent:
		return deletedBranchBranch(DeleteBranchBranchProviderRejected, sdkgo.FailureProviderRejection, "Bitbucket rejected branch deletion with HTTP "+strconv.Itoa(response.StatusCode))
	}
	return sdkgo.NewMutationBranch(DeleteBranchBranchDeleted, DeletedBranch{Workspace: input.Workspace, Repository: input.Repository, Name: input.Name}, nil, sdkgo.Receipt{})
}

func (client *Client) confirmBranchConflict(call sdkgo.Call, name, path string, successBranch, rejectedBranch, invalidBranch sdkgo.BranchID, matches func(Branch) bool) sdkgo.MutationAttempt[Branch] {
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return branchWriteBranch(name, invalidBranch, sdkgo.FailureLocalDefect, "branch confirmation could not be built")
	}
	request.Header.Set("Accept", "application/json")
	authorization := client.authorize(call, name, request, invalidBranch, rejectedBranch, invalidBranch)
	if authorization.failure != nil {
		return sdkgo.NewMutationBranch(authorization.branch, Branch{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[Branch](*failureFor(name, sdkgo.FailureTransport, "branch confirmation request failed"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return sdkgo.NewMutationRetry[Branch](*failureFor(name, sdkgo.FailureAvailability, "branch confirmation is unavailable"), 0)
	}
	if response.StatusCode != http.StatusOK {
		return branchWriteBranch(name, rejectedBranch, sdkgo.FailureProviderRejection, "Bitbucket did not confirm the branch")
	}
	branch, err := decodeBranchResponse(response, client.maxResponseBytes)
	if err != nil {
		return branchWriteBranch(name, invalidBranch, sdkgo.FailureProtocol, "Bitbucket returned an invalid branch confirmation")
	}
	if !matches(branch) {
		return branchWriteBranch(name, rejectedBranch, sdkgo.FailureProviderRejection, "existing branch points to a different commit")
	}
	return sdkgo.NewMutationBranch(successBranch, branch, nil, sdkgo.Receipt{})
}

func decodeBranch(body []byte) (Branch, error) {
	var branch Branch
	if err := json.Unmarshal(body, &branch); err != nil {
		return Branch{}, err
	}
	if !validBranchName(branch.Name) || branch.Target.Hash == "" {
		return Branch{}, errors.New("branch identity is incomplete")
	}
	return branch, nil
}

func decodeBranchPage(body []byte) (BranchPage, error) {
	var page BranchPage
	if err := json.Unmarshal(body, &page); err != nil {
		return BranchPage{}, err
	}
	if page.Values == nil {
		return BranchPage{}, errors.New("branch values are missing")
	}
	for _, branch := range page.Values {
		if !validBranchName(branch.Name) || branch.Target.Hash == "" {
			return BranchPage{}, errors.New("branch identity is incomplete")
		}
	}
	return page, nil
}

func decodeBranchResponse(response *http.Response, maxResponseBytes int64) (Branch, error) {
	body, err := providerhttp.ReadBoundedBody(response.Body, maxResponseBytes)
	if err != nil {
		return Branch{}, err
	}
	return decodeBranch(body)
}

func validBranchRefInput(input BranchRefInput) bool {
	return validPathSegment(input.Workspace) && validPathSegment(input.Repository) && validBranchName(input.Name)
}

func validBranchName(name string) bool {
	return name != "" && name != "@" && !strings.HasPrefix(name, "/") && !strings.HasSuffix(name, "/") && !strings.HasPrefix(name, "refs/heads/") && !strings.Contains(name, "//") && !strings.Contains(name, "..") && !strings.Contains(name, "@{") && !strings.ContainsAny(name, " \t\r\n~^:?*[\\")
}

func validCommitHash(hash string) bool {
	if len(hash) != 40 {
		return false
	}
	_, err := hex.DecodeString(hash)
	return err == nil
}

func branchesPath(workspace, repository string) string {
	return "/repositories/" + url.PathEscape(workspace) + "/" + url.PathEscape(repository) + "/refs/branches"
}

func branchPath(input BranchRefInput) string {
	return branchesPath(input.Workspace, input.Repository) + "/" + url.PathEscape(input.Name)
}

func branchWriteBranch(name string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[Branch] {
	return sdkgo.NewMutationBranch(branch, Branch{}, failureFor(name, kind, message), sdkgo.Receipt{})
}

func deletedBranchBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeletedBranch] {
	return sdkgo.NewMutationBranch(branch, DeletedBranch{}, failureFor("deleteBranch", kind, message), sdkgo.Receipt{})
}
