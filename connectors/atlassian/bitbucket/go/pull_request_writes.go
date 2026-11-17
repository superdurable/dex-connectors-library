// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bitbucket

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreatePullRequestInput selects two branches of one repository.
type CreatePullRequestInput struct {
	// Workspace is the Bitbucket workspace slug.
	Workspace string `json:"workspace"`
	// Repository is the repository slug.
	Repository string `json:"repository"`
	// Title is the new pull request title.
	Title string `json:"title"`
	// SourceBranch is the source branch name.
	SourceBranch string `json:"sourceBranch"`
	// DestinationBranch is the destination branch name.
	DestinationBranch string `json:"destinationBranch"`
}

// CreatePullRequestOperation creates one pull request with bounded possible sends.
type CreatePullRequestOperation struct{ client *Client }

// CreatePullRequest binds the Mutation to this client.
func (client *Client) CreatePullRequest() CreatePullRequestOperation {
	return CreatePullRequestOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (CreatePullRequestOperation) Definition() sdkgo.MutationDefinition {
	return CreatePullRequestDefinition
}

// IdempotencyKey records the Dex call identity; Bitbucket does not deduplicate by it.
func (CreatePullRequestOperation) IdempotencyKey(callID sdkgo.CallID, _ CreatePullRequestInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a pull request with a durable send cap.
func (operation CreatePullRequestOperation) Invoke(call sdkgo.Call, input CreatePullRequestInput) sdkgo.MutationAttempt[PullRequest] {
	const name = "createPullRequest"
	if !validPathSegment(input.Workspace) || !validPathSegment(input.Repository) || strings.TrimSpace(input.Title) == "" || len(input.Title) > 512 || strings.TrimSpace(input.SourceBranch) == "" || strings.TrimSpace(input.DestinationBranch) == "" || input.SourceBranch == input.DestinationBranch {
		return pullRequestBranch(name, CreatePullRequestBranchDefect, sdkgo.FailureValidation, "workspace, repository, title, or branches are invalid")
	}
	fence, err := singledispatch.NewFence(call, CreatePullRequestDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return pullRequestBranch(name, CreatePullRequestBranchDefect, sdkgo.FailureLocalDefect, "pull request dispatch fence could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(PullRequest{}, *failureFor(name, sdkgo.FailureTransport, "pull request creation could not be confirmed within the send cap"), sdkgo.Receipt{})
	}
	payload := struct {
		Title  string `json:"title"`
		Source struct {
			Branch struct {
				Name string `json:"name"`
			} `json:"branch"`
		} `json:"source"`
		Destination struct {
			Branch struct {
				Name string `json:"name"`
			} `json:"branch"`
		} `json:"destination"`
	}{Title: input.Title}
	payload.Source.Branch.Name = input.SourceBranch
	payload.Destination.Branch.Name = input.DestinationBranch
	body, err := json.Marshal(payload)
	if err != nil {
		return pullRequestBranch(name, CreatePullRequestBranchDefect, sdkgo.FailureLocalDefect, "pull request request could not be encoded")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+pullRequestsPath(input.Workspace, input.Repository), bytes.NewReader(body))
	if err != nil {
		return pullRequestBranch(name, CreatePullRequestBranchDefect, sdkgo.FailureLocalDefect, "pull request request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	authorization := operation.client.authorize(call, name, request, CreatePullRequestBranchDefect, CreatePullRequestBranchProviderRejected, CreatePullRequestBranchDefect)
	if authorization.failure != nil {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(PullRequest{}, *failureFor(name, sdkgo.FailureAuthentication, "an earlier pull request create may have succeeded"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationBranch(authorization.branch, PullRequest{}, authorization.failure, sdkgo.Receipt{})
	}
	var response *http.Response
	var requestError error
	err = fence.Dispatch(func(requestContext context.Context) error {
		response, requestError = operation.client.httpClient.Do(request.WithContext(requestContext))
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[PullRequest](*failureFor(name, sdkgo.FailureAvailability, "pull request create was not dispatched"), 0)
	}
	if requestError != nil {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(PullRequest{}, *failureFor(name, sdkgo.FailureTransport, "an earlier pull request create may have succeeded"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[PullRequest](*failureFor(name, sdkgo.FailureTransport, "pull request create request failed"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[PullRequest](*failureFor(name, sdkgo.FailureAvailability, "pull request resend checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[PullRequest](*failureFor(name, kind, "Bitbucket temporarily rejected pull request creation"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return sdkgo.NewMutationRetry[PullRequest](*failureFor(name, sdkgo.FailureAvailability, "pull request creation outcome is unknown"), 0)
	}
	if response.StatusCode != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(PullRequest{}, *failureFor(name, sdkgo.FailureProviderRejection, "an earlier pull request create may have succeeded"), sdkgo.Receipt{})
		}
		return pullRequestBranch(name, CreatePullRequestBranchProviderRejected, sdkgo.FailureProviderRejection, "Bitbucket rejected pull request creation with HTTP "+strconv.Itoa(response.StatusCode))
	}
	output, decodeError := decodePullRequestResponse(response, operation.client.maxResponseBytes)
	if decodeError != nil || output.Title != input.Title || output.State != "OPEN" {
		return sdkgo.NewMutationRetry[PullRequest](*failureFor(name, sdkgo.FailureProtocol, "Bitbucket returned an unusable created pull request"), 0)
	}
	return sdkgo.NewMutationBranch(CreatePullRequestBranchCreated, output, nil, sdkgo.Receipt{})
}

// PullRequestActionInput identifies an existing pull request.
type PullRequestActionInput struct {
	// Workspace is the Bitbucket workspace slug.
	Workspace string `json:"workspace"`
	// Repository is the repository slug.
	Repository string `json:"repository"`
	// PullRequestID is the positive repository-local ID.
	PullRequestID int64 `json:"pullRequestId"`
}

// MergePullRequestOperation merges an open pull request.
type MergePullRequestOperation struct{ client *Client }

// MergePullRequest binds the Mutation to this client.
func (client *Client) MergePullRequest() MergePullRequestOperation {
	return MergePullRequestOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (MergePullRequestOperation) Definition() sdkgo.MutationDefinition {
	return MergePullRequestDefinition
}

// IdempotencyKey records the Dex call identity for an absolute state transition.
func (MergePullRequestOperation) IdempotencyKey(callID sdkgo.CallID, _ PullRequestActionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke merges the pull request and confirms an already merged state on conflict.
func (operation MergePullRequestOperation) Invoke(call sdkgo.Call, input PullRequestActionInput) sdkgo.MutationAttempt[PullRequest] {
	return operation.client.executePullRequestAction(call, input, "mergePullRequest", "merge", "MERGED", MergePullRequestBranchMerged, MergePullRequestBranchNotFound, MergePullRequestBranchProviderRejected, MergePullRequestBranchInvalidResponse, MergePullRequestBranchDefect)
}

// DeclinePullRequestOperation declines an open pull request.
type DeclinePullRequestOperation struct{ client *Client }

// DeclinePullRequest binds the Mutation to this client.
func (client *Client) DeclinePullRequest() DeclinePullRequestOperation {
	return DeclinePullRequestOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (DeclinePullRequestOperation) Definition() sdkgo.MutationDefinition {
	return DeclinePullRequestDefinition
}

// IdempotencyKey records the Dex call identity for an absolute state transition.
func (DeclinePullRequestOperation) IdempotencyKey(callID sdkgo.CallID, _ PullRequestActionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke declines the pull request and confirms an already declined state on conflict.
func (operation DeclinePullRequestOperation) Invoke(call sdkgo.Call, input PullRequestActionInput) sdkgo.MutationAttempt[PullRequest] {
	return operation.client.executePullRequestAction(call, input, "declinePullRequest", "decline", "DECLINED", DeclinePullRequestBranchDeclined, DeclinePullRequestBranchNotFound, DeclinePullRequestBranchProviderRejected, DeclinePullRequestBranchInvalidResponse, DeclinePullRequestBranchDefect)
}

func (client *Client) executePullRequestAction(call sdkgo.Call, input PullRequestActionInput, name, action, expectedState string, successBranch, notFoundBranch, rejectedBranch, invalidBranch, defectBranch sdkgo.BranchID) sdkgo.MutationAttempt[PullRequest] {
	if !validPathSegment(input.Workspace) || !validPathSegment(input.Repository) || input.PullRequestID <= 0 {
		return pullRequestBranch(name, defectBranch, sdkgo.FailureValidation, "workspace, repository, or pull request ID is invalid")
	}
	path := pullRequestPath(input.Workspace, input.Repository, input.PullRequestID)
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+path+"/"+action, nil)
	if err != nil {
		return pullRequestBranch(name, defectBranch, sdkgo.FailureLocalDefect, "pull request action could not be built")
	}
	request.Header.Set("Accept", "application/json")
	authorization := client.authorize(call, name, request, defectBranch, rejectedBranch, invalidBranch)
	if authorization.failure != nil {
		return sdkgo.NewMutationBranch(authorization.branch, PullRequest{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[PullRequest](*failureFor(name, sdkgo.FailureTransport, "pull request action request failed"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[PullRequest](*failureFor(name, sdkgo.FailureRateLimit, "Bitbucket rate limited pull request action"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewMutationRetry[PullRequest](*failureFor(name, sdkgo.FailureAvailability, "pull request action is unavailable"), 0)
	case response.StatusCode == http.StatusConflict:
		return client.confirmPullRequestState(call, name, path, input.PullRequestID, expectedState, successBranch, rejectedBranch, invalidBranch)
	case response.StatusCode == http.StatusNotFound:
		return pullRequestBranch(name, notFoundBranch, sdkgo.FailureNotFound, "pull request was not found")
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return pullRequestBranch(name, rejectedBranch, sdkgo.FailureAuthorization, "Bitbucket denied pull request action")
	case response.StatusCode != http.StatusOK:
		return pullRequestBranch(name, rejectedBranch, sdkgo.FailureProviderRejection, "Bitbucket rejected pull request action with HTTP "+strconv.Itoa(response.StatusCode))
	}
	output, err := decodePullRequestResponse(response, client.maxResponseBytes)
	if err != nil || output.ID != input.PullRequestID || output.State != expectedState {
		return pullRequestBranch(name, invalidBranch, sdkgo.FailureProtocol, "Bitbucket returned a different pull request state")
	}
	return sdkgo.NewMutationBranch(successBranch, output, nil, sdkgo.Receipt{})
}

func (client *Client) confirmPullRequestState(call sdkgo.Call, name, path string, pullRequestID int64, expectedState string, successBranch, rejectedBranch, invalidBranch sdkgo.BranchID) sdkgo.MutationAttempt[PullRequest] {
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return pullRequestBranch(name, invalidBranch, sdkgo.FailureLocalDefect, "pull request confirmation could not be built")
	}
	request.Header.Set("Accept", "application/json")
	authorization := client.authorize(call, name, request, invalidBranch, rejectedBranch, invalidBranch)
	if authorization.failure != nil {
		return sdkgo.NewMutationBranch(authorization.branch, PullRequest{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[PullRequest](*failureFor(name, sdkgo.FailureTransport, "pull request confirmation failed"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[PullRequest](*failureFor(name, sdkgo.FailureRateLimit, "Bitbucket rate limited pull request confirmation"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return sdkgo.NewMutationRetry[PullRequest](*failureFor(name, sdkgo.FailureAvailability, "pull request confirmation is unavailable"), 0)
	}
	if response.StatusCode != http.StatusOK {
		return pullRequestBranch(name, rejectedBranch, sdkgo.FailureProviderRejection, "Bitbucket did not confirm pull request state")
	}
	output, err := decodePullRequestResponse(response, client.maxResponseBytes)
	if err != nil || output.ID != pullRequestID {
		return pullRequestBranch(name, invalidBranch, sdkgo.FailureProtocol, "Bitbucket returned an invalid pull request confirmation")
	}
	if output.State != expectedState {
		return pullRequestBranch(name, rejectedBranch, sdkgo.FailureProviderRejection, "Bitbucket rejected the pull request action")
	}
	return sdkgo.NewMutationBranch(successBranch, output, nil, sdkgo.Receipt{})
}

func decodePullRequestResponse(response *http.Response, maxResponseBytes int64) (PullRequest, error) {
	body, err := providerhttp.ReadBoundedBody(response.Body, maxResponseBytes)
	if err != nil {
		return PullRequest{}, err
	}
	return decodePullRequest(body)
}

func pullRequestBranch(name string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[PullRequest] {
	return sdkgo.NewMutationBranch(branch, PullRequest{}, failureFor(name, kind, message), sdkgo.Receipt{})
}

func pullRequestsPath(workspace, repository string) string {
	return "/repositories/" + url.PathEscape(workspace) + "/" + url.PathEscape(repository) + "/pullrequests"
}
