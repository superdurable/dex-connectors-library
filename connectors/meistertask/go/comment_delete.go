// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meistertask

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// DeleteCommentInput identifies the comment to remove.
type DeleteCommentInput struct {
	// CommentID is a positive decimal comment ID.
	CommentID string `json:"commentId"`
}

// DeletedComment confirms the requested comment ID.
type DeletedComment struct {
	// CommentID is the ID submitted for deletion.
	CommentID string `json:"commentId"`
}

// DeleteCommentOperation removes a comment by ID.
type DeleteCommentOperation struct{ client *Client }

// DeleteComment binds the Mutation to this client.
func (client *Client) DeleteComment() DeleteCommentOperation { return DeleteCommentOperation{client} }

// Definition returns the generated delete Mutation contract.
func (DeleteCommentOperation) Definition() sdkgo.MutationDefinition { return DeleteCommentDefinition }

// IdempotencyKey supplies a stable call identity for deletion.
func (DeleteCommentOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteCommentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes one comment; repeated deletes cannot create a duplicate effect.
func (operation DeleteCommentOperation) Invoke(call sdkgo.Call, input DeleteCommentInput) sdkgo.MutationAttempt[DeletedComment] {
	if !validTaskID(input.CommentID) {
		return commentMutationBranch("deleteComment", DeleteCommentBranchDefect, DeletedComment{}, sdkgo.FailureValidation, "comment ID is invalid")
	}
	response := operation.client.delete(call, "/comments/"+input.CommentID)
	if errors.Is(response.err, errCredential) {
		return commentMutationBranch("deleteComment", DeleteCommentBranchDefect, DeletedComment{}, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[DeletedComment](commentFailure("deleteComment", sdkgo.FailureTransport, "MeisterTask request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[DeletedComment](commentFailure("deleteComment", sdkgo.FailureRateLimit, "MeisterTask rate limited the delete"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[DeletedComment](commentFailure("deleteComment", sdkgo.FailureAvailability, "MeisterTask delete outcome is not confirmed"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return commentMutationBranch("deleteComment", DeleteCommentBranchNotFound, DeletedComment{}, sdkgo.FailureNotFound, "MeisterTask did not expose the comment")
	}
	if response.status != http.StatusOK && response.status != http.StatusNoContent {
		return commentMutationBranch("deleteComment", DeleteCommentBranchProviderRejected, DeletedComment{}, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[DeletedComment](commentFailure("deleteComment", sdkgo.FailureTransport, "MeisterTask delete response was interrupted"), 0)
	}
	return sdkgo.NewMutationBranch(DeleteCommentBranchDeleted, DeletedComment{CommentID: input.CommentID}, nil, sdkgo.Receipt{})
}

func (client *Client) delete(call sdkgo.Call, path string) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	apiToken := credentials.APIToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(apiToken) {
		return providerResponse{err: errCredential}
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodDelete, client.baseURL+path, nil)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiToken)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	_, err = providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{status: response.StatusCode, retryAfter: retryAfter, err: err}
}

func commentMutationBranch[Output any](operation string, branch sdkgo.BranchID, value Output, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[Output] {
	failure := commentFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}
