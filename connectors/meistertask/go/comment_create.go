// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meistertask

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateCommentInput adds Markdown text to one task.
type CreateCommentInput struct {
	// TaskID is the positive parent task ID.
	TaskID string `json:"taskId"`
	// Text is the nonempty Markdown comment text.
	Text string `json:"text"`
}

// CreateCommentOperation creates a comment through a bounded dispatch fence.
type CreateCommentOperation struct{ client *Client }

// CreateComment binds the Mutation to this client.
func (client *Client) CreateComment() CreateCommentOperation { return CreateCommentOperation{client} }

// Definition returns the generated comment Mutation contract.
func (CreateCommentOperation) Definition() sdkgo.MutationDefinition { return CreateCommentDefinition }

// IdempotencyKey records the stable call identity; MeisterTask does not deduplicate it.
func (CreateCommentOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateCommentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a comment, repeating unknown sends only within the configured cap.
func (operation CreateCommentOperation) Invoke(call sdkgo.Call, input CreateCommentInput) sdkgo.MutationAttempt[CommentSummary] {
	if !validTaskID(input.TaskID) || strings.TrimSpace(input.Text) == "" || len(input.Text) > 65536 {
		return commentMutationBranch("createComment", CreateCommentBranchDefect, CommentSummary{}, sdkgo.FailureValidation, "task ID or comment text is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateCommentDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return commentMutationBranch("createComment", CreateCommentBranchDefect, CommentSummary{}, sdkgo.FailureLocalDefect, "comment dispatch fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return createCommentUncertain(sdkgo.FailureTransport, "an earlier comment create may have succeeded")
	}
	// Repeated text is valid, so a list lookup cannot confirm this call's result.
	canResendUnknown := decision.MaximumPriorSends+1 < call.MaxSends()
	hasEarlierUnknownSend := decision.MaximumPriorSends > 0
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		if hasEarlierUnknownSend {
			return createCommentUncertain(sdkgo.FailureAuthentication, "an earlier comment create may have succeeded")
		}
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
			return createCommentRetry(sdkgo.FailureAvailability, "credential checkpoint is unavailable", 0)
		}
		return commentMutationBranch("createComment", CreateCommentBranchDefect, CommentSummary{}, sdkgo.FailureAuthentication, "MeisterTask credentials are unavailable or invalid")
	}
	apiToken := credentials.APIToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(apiToken) {
		return commentMutationBranch("createComment", CreateCommentBranchDefect, CommentSummary{}, sdkgo.FailureAuthentication, "MeisterTask credentials are invalid")
	}
	payload, err := json.Marshal(struct {
		Text string `json:"text"`
	}{input.Text})
	if err != nil {
		return commentMutationBranch("createComment", CreateCommentBranchDefect, CommentSummary{}, sdkgo.FailureLocalDefect, "comment create could not be encoded")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/tasks/"+input.TaskID+"/comments", bytes.NewReader(payload))
	if err != nil {
		return commentMutationBranch("createComment", CreateCommentBranchDefect, CommentSummary{}, sdkgo.FailureLocalDefect, "comment create request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiToken)
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return commentMutationBranch("createComment", CreateCommentBranchDefect, CommentSummary{}, sdkgo.FailureLocalDefect, "comment create request is invalid")
	}
	dispatch := &createCommentDispatch{client: operation.client.httpClient, request: request}
	if err := fence.Dispatch(dispatch.send); err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return createCommentRetry(sdkgo.FailureTransport, "comment dispatch was not started", 0)
		}
		if !dispatch.didGetConnection.Load() {
			if checkpointErr := fence.RecordCanResend(); checkpointErr == nil {
				return createCommentRetry(sdkgo.FailureTransport, "MeisterTask could not be reached before sending", 0)
			}
		}
		return retryOrUncertainCreateComment(canResendUnknown, sdkgo.FailureTransport, "comment create outcome is unknown", 0)
	}
	response := dispatch.response
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return createCommentRetry(sdkgo.FailureAvailability, "retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.StatusCode == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return createCommentRetry(kind, "MeisterTask did not process the comment create", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return retryOrUncertainCreateComment(canResendUnknown, sdkgo.FailureAvailability, "comment create outcome is unknown", retryAfter)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if hasEarlierUnknownSend {
			return createCommentUncertain(sdkgo.FailureProviderRejection, "an earlier comment create may have succeeded")
		}
		return commentMutationBranch("createComment", CreateCommentBranchProviderRejected, CommentSummary{}, sdkgo.FailureProviderRejection, "MeisterTask rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return retryOrUncertainCreateComment(canResendUnknown, sdkgo.FailureTransport, "comment create response could not be confirmed", 0)
	}
	comment, err := decodeComment(body, "")
	if err != nil || comment.TaskID != input.TaskID {
		return retryOrUncertainCreateComment(canResendUnknown, sdkgo.FailureProtocol, "comment create response was incomplete", 0)
	}
	if err := fence.RecordProviderID(comment.ID); err != nil {
		return createCommentUncertain(sdkgo.FailureLocalDefect, "comment ID could not be checkpointed")
	}
	return sdkgo.NewMutationBranch(CreateCommentBranchCreated, comment, nil, sdkgo.Receipt{})
}

type createCommentDispatch struct {
	client           *http.Client
	request          *http.Request
	response         *http.Response
	didGetConnection atomic.Bool
}

func (dispatch *createCommentDispatch) send(requestContext context.Context) error {
	trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { dispatch.didGetConnection.Store(true) }}
	response, err := dispatch.client.Do(dispatch.request.WithContext(httptrace.WithClientTrace(requestContext, trace)))
	dispatch.response = response
	return err
}

func createCommentRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[CommentSummary] {
	return sdkgo.NewMutationRetry[CommentSummary](commentFailure("createComment", kind, message), retryAfter)
}
func createCommentUncertain(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CommentSummary] {
	return sdkgo.NewMutationUncertain(CommentSummary{}, commentFailure("createComment", kind, message), sdkgo.Receipt{})
}
func retryOrUncertainCreateComment(canResend bool, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[CommentSummary] {
	if canResend {
		return createCommentRetry(kind, message, retryAfter)
	}
	return createCommentUncertain(kind, message)
}
