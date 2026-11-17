// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bitbucket

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreatePullRequestCommentInput supplies Markdown text for a new PR comment.
type CreatePullRequestCommentInput struct {
	// Workspace is the Bitbucket workspace slug.
	Workspace string `json:"workspace"`
	// Repository is the repository slug.
	Repository string `json:"repository"`
	// PullRequestID is the repository-local pull request ID.
	PullRequestID int64 `json:"pullRequestId"`
	// Raw is the new comment's Markdown text.
	Raw string `json:"raw"`
}

// CreatePullRequestCommentOperation creates a comment with bounded possible sends.
type CreatePullRequestCommentOperation struct{ client *Client }

// CreatePullRequestComment binds the Mutation to this client.
func (client *Client) CreatePullRequestComment() CreatePullRequestCommentOperation {
	return CreatePullRequestCommentOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (CreatePullRequestCommentOperation) Definition() sdkgo.MutationDefinition {
	return CreatePullRequestCommentDefinition
}

// IdempotencyKey records the Dex call identity; Bitbucket does not deduplicate by it.
func (CreatePullRequestCommentOperation) IdempotencyKey(callID sdkgo.CallID, _ CreatePullRequestCommentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a comment within a durable send cap.
func (operation CreatePullRequestCommentOperation) Invoke(call sdkgo.Call, input CreatePullRequestCommentInput) sdkgo.MutationAttempt[PullRequestComment] {
	const name = "createPullRequestComment"
	identity := PullRequestActionInput{Workspace: input.Workspace, Repository: input.Repository, PullRequestID: input.PullRequestID}
	if !validPullRequestActionInput(identity) || strings.TrimSpace(input.Raw) == "" {
		return commentBranch(name, CreatePullRequestCommentBranchDefect, sdkgo.FailureValidation, "pull request identity or comment text is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreatePullRequestCommentDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return commentBranch(name, CreatePullRequestCommentBranchDefect, sdkgo.FailureLocalDefect, "comment dispatch fence could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(PullRequestComment{}, *failureFor(name, sdkgo.FailureTransport, "comment creation could not be confirmed within the send cap"), sdkgo.Receipt{})
	}
	body, err := json.Marshal(map[string]any{"content": map[string]string{"raw": input.Raw}})
	if err != nil {
		return commentBranch(name, CreatePullRequestCommentBranchDefect, sdkgo.FailureLocalDefect, "comment request could not be encoded")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+pullRequestCommentsPath(identity), bytes.NewReader(body))
	if err != nil {
		return commentBranch(name, CreatePullRequestCommentBranchDefect, sdkgo.FailureLocalDefect, "comment request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	authorization := operation.client.authorize(call, name, request, CreatePullRequestCommentBranchDefect, CreatePullRequestCommentBranchProviderRejected, CreatePullRequestCommentBranchDefect)
	if authorization.failure != nil {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(PullRequestComment{}, *failureFor(name, sdkgo.FailureAuthentication, "an earlier comment create may have succeeded"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationBranch(authorization.branch, PullRequestComment{}, authorization.failure, sdkgo.Receipt{})
	}
	var response *http.Response
	var requestError error
	err = fence.Dispatch(func(requestContext context.Context) error {
		response, requestError = operation.client.httpClient.Do(request.WithContext(requestContext))
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[PullRequestComment](*failureFor(name, sdkgo.FailureAvailability, "comment create was not dispatched"), 0)
	}
	if requestError != nil {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(PullRequestComment{}, *failureFor(name, sdkgo.FailureTransport, "an earlier comment create may have succeeded"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[PullRequestComment](*failureFor(name, sdkgo.FailureTransport, "comment create request failed"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[PullRequestComment](*failureFor(name, sdkgo.FailureAvailability, "comment resend checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[PullRequestComment](*failureFor(name, kind, "Bitbucket temporarily rejected comment creation"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return sdkgo.NewMutationRetry[PullRequestComment](*failureFor(name, sdkgo.FailureAvailability, "comment creation outcome is unknown"), 0)
	}
	if response.StatusCode != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(PullRequestComment{}, *failureFor(name, sdkgo.FailureProviderRejection, "an earlier comment create may have succeeded"), sdkgo.Receipt{})
		}
		return commentBranch(name, CreatePullRequestCommentBranchProviderRejected, sdkgo.FailureProviderRejection, "Bitbucket rejected comment creation with HTTP "+strconv.Itoa(response.StatusCode))
	}
	output, err := decodePullRequestCommentResponse(response, operation.client.maxResponseBytes)
	if err != nil || output.Content.Raw != input.Raw {
		return sdkgo.NewMutationRetry[PullRequestComment](*failureFor(name, sdkgo.FailureProtocol, "Bitbucket returned an unusable created comment"), 0)
	}
	return sdkgo.NewMutationBranch(CreatePullRequestCommentBranchCreated, output, nil, sdkgo.Receipt{})
}
