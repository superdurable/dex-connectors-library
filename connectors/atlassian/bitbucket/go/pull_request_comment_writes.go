// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bitbucket

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UpdatePullRequestCommentInput replaces the Markdown text of one comment.
type UpdatePullRequestCommentInput struct {
	// Workspace is the Bitbucket workspace slug.
	Workspace string `json:"workspace"`
	// Repository is the repository slug.
	Repository string `json:"repository"`
	// PullRequestID is the repository-local pull request ID.
	PullRequestID int64 `json:"pullRequestId"`
	// CommentID is the positive comment ID.
	CommentID int64 `json:"commentId"`
	// Raw is the complete replacement Markdown text.
	Raw string `json:"raw"`
}

// UpdatePullRequestCommentOperation replaces a comment's text.
type UpdatePullRequestCommentOperation struct{ client *Client }

// UpdatePullRequestComment binds the Mutation to this client.
func (client *Client) UpdatePullRequestComment() UpdatePullRequestCommentOperation {
	return UpdatePullRequestCommentOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (UpdatePullRequestCommentOperation) Definition() sdkgo.MutationDefinition {
	return UpdatePullRequestCommentDefinition
}

// IdempotencyKey records the Dex call identity for an absolute text replacement.
func (UpdatePullRequestCommentOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdatePullRequestCommentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces a comment's complete Markdown source.
func (operation UpdatePullRequestCommentOperation) Invoke(call sdkgo.Call, input UpdatePullRequestCommentInput) sdkgo.MutationAttempt[PullRequestComment] {
	const name = "updatePullRequestComment"
	id := PullRequestCommentIDInput{Workspace: input.Workspace, Repository: input.Repository, PullRequestID: input.PullRequestID, CommentID: input.CommentID}
	if !validPullRequestCommentIDInput(id) || strings.TrimSpace(input.Raw) == "" {
		return commentBranch(name, UpdatePullRequestCommentBranchDefect, sdkgo.FailureValidation, "comment identity or text is invalid")
	}
	return operation.client.executeCommentWrite(call, name, http.MethodPut, pullRequestCommentPath(id), map[string]any{"content": map[string]string{"raw": input.Raw}}, UpdatePullRequestCommentBranchUpdated, UpdatePullRequestCommentBranchNotFound, UpdatePullRequestCommentBranchProviderRejected, UpdatePullRequestCommentBranchInvalidResponse, UpdatePullRequestCommentBranchDefect, func(comment PullRequestComment) bool {
		return comment.ID == input.CommentID && comment.Content.Raw == input.Raw
	}, false)
}

// DeletedPullRequestComment identifies a successfully deleted comment.
type DeletedPullRequestComment struct {
	// Workspace is the Bitbucket workspace slug.
	Workspace string `json:"workspace"`
	// Repository is the repository slug.
	Repository string `json:"repository"`
	// PullRequestID is the repository-local pull request ID.
	PullRequestID int64 `json:"pullRequestId"`
	// CommentID is the deleted comment ID.
	CommentID int64 `json:"commentId"`
}

// DeletePullRequestCommentOperation deletes a comment.
type DeletePullRequestCommentOperation struct{ client *Client }

// DeletePullRequestComment binds the Mutation to this client.
func (client *Client) DeletePullRequestComment() DeletePullRequestCommentOperation {
	return DeletePullRequestCommentOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (DeletePullRequestCommentOperation) Definition() sdkgo.MutationDefinition {
	return DeletePullRequestCommentDefinition
}

// IdempotencyKey records the Dex call identity for a delete.
func (DeletePullRequestCommentOperation) IdempotencyKey(callID sdkgo.CallID, _ PullRequestCommentIDInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the selected comment.
func (operation DeletePullRequestCommentOperation) Invoke(call sdkgo.Call, input PullRequestCommentIDInput) sdkgo.MutationAttempt[DeletedPullRequestComment] {
	const name = "deletePullRequestComment"
	if !validPullRequestCommentIDInput(input) {
		return deletedCommentBranch(DeletePullRequestCommentBranchDefect, sdkgo.FailureValidation, "comment identity is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodDelete, operation.client.baseURL+pullRequestCommentPath(input), nil)
	if err != nil {
		return deletedCommentBranch(DeletePullRequestCommentBranchDefect, sdkgo.FailureLocalDefect, "comment delete request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	authorization := operation.client.authorize(call, name, request, DeletePullRequestCommentBranchDefect, DeletePullRequestCommentBranchProviderRejected, DeletePullRequestCommentBranchDefect)
	if authorization.failure != nil {
		return sdkgo.NewMutationBranch(authorization.branch, DeletedPullRequestComment{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[DeletedPullRequestComment](*failureFor(name, sdkgo.FailureTransport, "comment delete request failed"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[DeletedPullRequestComment](*failureFor(name, sdkgo.FailureRateLimit, "Bitbucket rate limited comment deletion"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewMutationRetry[DeletedPullRequestComment](*failureFor(name, sdkgo.FailureAvailability, "comment deletion is unavailable"), 0)
	case response.StatusCode == http.StatusNotFound:
		return deletedCommentBranch(DeletePullRequestCommentBranchNotFound, sdkgo.FailureNotFound, "comment was not found")
	case response.StatusCode != http.StatusNoContent:
		return deletedCommentBranch(DeletePullRequestCommentBranchProviderRejected, sdkgo.FailureProviderRejection, "Bitbucket rejected comment deletion with HTTP "+strconv.Itoa(response.StatusCode))
	}
	return sdkgo.NewMutationBranch(DeletePullRequestCommentBranchDeleted, DeletedPullRequestComment{Workspace: input.Workspace, Repository: input.Repository, PullRequestID: input.PullRequestID, CommentID: input.CommentID}, nil, sdkgo.Receipt{})
}

// ResolvePullRequestCommentOperation marks a comment resolved.
type ResolvePullRequestCommentOperation struct{ client *Client }

// ResolvePullRequestComment binds the Mutation to this client.
func (client *Client) ResolvePullRequestComment() ResolvePullRequestCommentOperation {
	return ResolvePullRequestCommentOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (ResolvePullRequestCommentOperation) Definition() sdkgo.MutationDefinition {
	return ResolvePullRequestCommentDefinition
}

// IdempotencyKey records the Dex call identity for an absolute resolution state.
func (ResolvePullRequestCommentOperation) IdempotencyKey(callID sdkgo.CallID, _ PullRequestCommentIDInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke resolves the selected comment thread.
func (operation ResolvePullRequestCommentOperation) Invoke(call sdkgo.Call, input PullRequestCommentIDInput) sdkgo.MutationAttempt[PullRequestComment] {
	const name = "resolvePullRequestComment"
	if !validPullRequestCommentIDInput(input) {
		return commentBranch(name, ResolvePullRequestCommentBranchDefect, sdkgo.FailureValidation, "comment identity is invalid")
	}
	return operation.client.executeCommentWrite(call, name, http.MethodPost, pullRequestCommentPath(input)+"/resolve", nil, ResolvePullRequestCommentBranchResolved, ResolvePullRequestCommentBranchNotFound, ResolvePullRequestCommentBranchProviderRejected, ResolvePullRequestCommentBranchInvalidResponse, ResolvePullRequestCommentBranchDefect, func(comment PullRequestComment) bool {
		return comment.ID == input.CommentID && comment.Resolution != nil
	}, true)
}

// UnresolvePullRequestCommentOperation reopens a resolved comment.
type UnresolvePullRequestCommentOperation struct{ client *Client }

// UnresolvePullRequestComment binds the Mutation to this client.
func (client *Client) UnresolvePullRequestComment() UnresolvePullRequestCommentOperation {
	return UnresolvePullRequestCommentOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (UnresolvePullRequestCommentOperation) Definition() sdkgo.MutationDefinition {
	return UnresolvePullRequestCommentDefinition
}

// IdempotencyKey records the Dex call identity for an absolute resolution state.
func (UnresolvePullRequestCommentOperation) IdempotencyKey(callID sdkgo.CallID, _ PullRequestCommentIDInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reopens the selected comment thread.
func (operation UnresolvePullRequestCommentOperation) Invoke(call sdkgo.Call, input PullRequestCommentIDInput) sdkgo.MutationAttempt[PullRequestComment] {
	const name = "unresolvePullRequestComment"
	if !validPullRequestCommentIDInput(input) {
		return commentBranch(name, UnresolvePullRequestCommentBranchDefect, sdkgo.FailureValidation, "comment identity is invalid")
	}
	return operation.client.executeCommentWrite(call, name, http.MethodDelete, pullRequestCommentPath(input)+"/resolve", nil, UnresolvePullRequestCommentBranchUnresolved, UnresolvePullRequestCommentBranchNotFound, UnresolvePullRequestCommentBranchProviderRejected, UnresolvePullRequestCommentBranchInvalidResponse, UnresolvePullRequestCommentBranchDefect, func(comment PullRequestComment) bool {
		return comment.ID == input.CommentID && comment.Resolution == nil
	}, true)
}

func (client *Client) executeCommentWrite(call sdkgo.Call, name, method, path string, payload any, successBranch, notFoundBranch, rejectedBranch, invalidBranch, defectBranch sdkgo.BranchID, matches func(PullRequestComment) bool, confirmConflict bool) sdkgo.MutationAttempt[PullRequestComment] {
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return commentBranch(name, defectBranch, sdkgo.FailureLocalDefect, "comment payload could not be encoded")
		}
	}
	request, err := http.NewRequestWithContext(call.Context, method, client.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return commentBranch(name, defectBranch, sdkgo.FailureLocalDefect, "comment request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	authorization := client.authorize(call, name, request, defectBranch, rejectedBranch, invalidBranch)
	if authorization.failure != nil {
		return sdkgo.NewMutationBranch(authorization.branch, PullRequestComment{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[PullRequestComment](*failureFor(name, sdkgo.FailureTransport, "comment write request failed"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[PullRequestComment](*failureFor(name, sdkgo.FailureRateLimit, "Bitbucket rate limited comment write"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewMutationRetry[PullRequestComment](*failureFor(name, sdkgo.FailureAvailability, "comment write is unavailable"), 0)
	case confirmConflict && (response.StatusCode == http.StatusConflict || response.StatusCode == http.StatusNoContent):
		commentPath := strings.TrimSuffix(path, "/resolve")
		return client.confirmCommentState(call, name, commentPath, successBranch, rejectedBranch, invalidBranch, matches)
	case response.StatusCode == http.StatusNotFound:
		return commentBranch(name, notFoundBranch, sdkgo.FailureNotFound, "comment was not found")
	case response.StatusCode != http.StatusOK:
		return commentBranch(name, rejectedBranch, sdkgo.FailureProviderRejection, "Bitbucket rejected comment write with HTTP "+strconv.Itoa(response.StatusCode))
	}
	output, err := decodePullRequestCommentResponse(response, client.maxResponseBytes)
	if err != nil || !matches(output) {
		return commentBranch(name, invalidBranch, sdkgo.FailureProtocol, "Bitbucket returned a different comment state")
	}
	return sdkgo.NewMutationBranch(successBranch, output, nil, sdkgo.Receipt{})
}

func (client *Client) confirmCommentState(call sdkgo.Call, name, path string, successBranch, rejectedBranch, invalidBranch sdkgo.BranchID, matches func(PullRequestComment) bool) sdkgo.MutationAttempt[PullRequestComment] {
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return commentBranch(name, invalidBranch, sdkgo.FailureLocalDefect, "comment confirmation could not be built")
	}
	request.Header.Set("Accept", "application/json")
	authorization := client.authorize(call, name, request, invalidBranch, rejectedBranch, invalidBranch)
	if authorization.failure != nil {
		return sdkgo.NewMutationBranch(authorization.branch, PullRequestComment{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[PullRequestComment](*failureFor(name, sdkgo.FailureTransport, "comment confirmation request failed"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
		return sdkgo.NewMutationRetry[PullRequestComment](*failureFor(name, sdkgo.FailureAvailability, "comment confirmation is unavailable"), 0)
	}
	if response.StatusCode != http.StatusOK {
		return commentBranch(name, rejectedBranch, sdkgo.FailureProviderRejection, "Bitbucket did not confirm comment state")
	}
	output, err := decodePullRequestCommentResponse(response, client.maxResponseBytes)
	if err != nil {
		return commentBranch(name, invalidBranch, sdkgo.FailureProtocol, "Bitbucket returned an invalid comment confirmation")
	}
	if !matches(output) {
		return commentBranch(name, rejectedBranch, sdkgo.FailureProviderRejection, "Bitbucket rejected the comment action")
	}
	return sdkgo.NewMutationBranch(successBranch, output, nil, sdkgo.Receipt{})
}

func decodePullRequestCommentResponse(response *http.Response, maxResponseBytes int64) (PullRequestComment, error) {
	body, err := providerhttp.ReadBoundedBody(response.Body, maxResponseBytes)
	if err != nil {
		return PullRequestComment{}, err
	}
	return decodePullRequestComment(body)
}

func commentBranch(name string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[PullRequestComment] {
	return sdkgo.NewMutationBranch(branch, PullRequestComment{}, failureFor(name, kind, message), sdkgo.Receipt{})
}

func deletedCommentBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeletedPullRequestComment] {
	return sdkgo.NewMutationBranch(branch, DeletedPullRequestComment{}, failureFor("deletePullRequestComment", kind, message), sdkgo.Receipt{})
}
