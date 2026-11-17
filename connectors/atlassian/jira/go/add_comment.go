// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const (
	addCommentOperationID    = "addComment"
	addCommentFailureSubject = "comment"
)

// AddCommentInput describes one comment on one issue.
type AddCommentInput struct {
	// IssueIDOrKey is an issue key such as OPS-441 or a numeric issue ID.
	IssueIDOrKey string `json:"issueIdOrKey"`
	// Body is plain text of at most 32767 characters. Blank lines separate paragraphs and other line
	// breaks are kept; the text is never read as markup.
	Body string `json:"body"`
}

// AddCommentOutput identifies the added comment. On notFound, providerRejected, and uncertain,
// CommentID is empty and IssueIDOrKey echoes the request.
type AddCommentOutput struct {
	// IssueIDOrKey echoes the requested issue.
	IssueIDOrKey string `json:"issueIdOrKey"`
	// CommentID is the new comment's numeric ID.
	CommentID string `json:"commentId,omitempty"`
	// AuthorAccountID is the account that wrote the comment, the authorized user.
	AuthorAccountID string `json:"authorAccountId,omitempty"`
	// CreatedAt is when Jira stored the comment.
	CreatedAt time.Time `json:"createdAt"`
	// RejectedFieldIDs lists the field IDs Jira named in a rejection.
	RejectedFieldIDs []string `json:"rejectedFieldIds,omitempty"`
}

// AddCommentOperation implements the addComment Mutation.
type AddCommentOperation struct{ client *Client }

type addCommentRequestBody struct {
	Body       documentNode       `json:"body"`
	Properties []jiraCallProperty `json:"properties"`
}

type commentResource struct {
	ID         string             `json:"id"`
	Created    string             `json:"created"`
	Author     *accountResource   `json:"author"`
	Properties []jiraCallProperty `json:"properties,omitempty"`
}

// Definition returns the immutable connector operation definition.
func (AddCommentOperation) Definition() sdkgo.MutationDefinition { return AddCommentDefinition }

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID.
// Jira accepts no idempotency key, so it is never sent and cannot deduplicate a repeated comment.
func (AddCommentOperation) IdempotencyKey(callID sdkgo.CallID, _ AddCommentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke adds one comment behind a shared dispatch fence and reconciles a possibly accepted write.
func (operation AddCommentOperation) Invoke(call sdkgo.Call, input AddCommentInput) sdkgo.MutationAttempt[AddCommentOutput] {
	client := operation.client
	requested := AddCommentOutput{IssueIDOrKey: input.IssueIDOrKey}
	issueIDOrKey, err := validateIssueIDOrKey(input.IssueIDOrKey)
	if err == nil {
		requested.IssueIDOrKey = issueIDOrKey
		err = validatePlainText(input.Body, "body", true)
	}
	if err != nil {
		return sdkgo.NewMutationBranch(AddCommentBranchDefect, requested, failurePointer(addCommentOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, AddCommentDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(AddCommentBranchDefect, requested, failurePointer(addCommentOperationID, sdkgo.FailureLocalDefect, "Jira dispatch fence could not be constructed"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationRetry[AddCommentOutput](newFailure(addCommentOperationID, sdkgo.FailureAvailability, "Jira dispatch checkpoint is unreadable, so nothing was sent in this attempt"), 0)
	}
	policy := newSendCapPolicy(call, decision)
	session, cancel, sessionErr := client.startSession(call, addCommentOperationID)
	if sessionErr != nil {
		if decision.Kind == singledispatch.SendAllowed {
			if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
				return sdkgo.NewMutationRetry[AddCommentOutput](newFailure(addCommentOperationID, sdkgo.FailureAvailability, "Jira setup failed and the not-sent checkpoint could not be recorded"), sessionErr.retryAfter)
			}
		}
		if sessionErr.isRetryable {
			return sdkgo.NewMutationRetry[AddCommentOutput](sessionErr.failure, sessionErr.retryAfter)
		}
		return defectOrSelectUncertain(policy, AddCommentBranchDefect, requested, sessionErr.failure, sdkgo.Receipt{})
	}
	defer cancel()
	marker := jiraCallPropertyForCall(call)
	receipt := client.emptyReceipt(session)
	possibleEarlierSendFailure := newFailure(addCommentOperationID, sdkgo.FailureAvailability, "Jira comment dispatch may have succeeded")
	switch decision.Kind {
	case singledispatch.ReadBackRequired:
		return client.reconcileAddedComment(session, requested, issueIDOrKey, marker, decision.Marker.ProviderID, sendCapPolicy{}, possibleEarlierSendFailure, receipt)
	case singledispatch.ReadBackThenResendAllowed:
		if decision.Marker.ProviderID != "" {
			return client.reconcileAddedComment(session, requested, issueIDOrKey, marker, decision.Marker.ProviderID, sendCapPolicy{}, possibleEarlierSendFailure, receipt)
		}
		comment, status := client.findAddedComment(session, issueIDOrKey, marker, "")
		switch status {
		case writeReadbackFound:
			return addedCommentBranch(requested, comment, receipt)
		case writeReadbackRetry:
			return sdkgo.NewMutationRetry[AddCommentOutput](newFailure(addCommentOperationID, sdkgo.FailureAvailability, "Jira comment read-back is temporarily unavailable"), 0)
		}
	}
	attempt := &jiraWriteAttempt{client: client, session: session, request: jiraRequest{
		method: http.MethodPost, path: issuePath(issueIDOrKey) + "/comment",
		payload: addCommentRequestBody{Body: convertPlainTextToDocument(input.Body), Properties: []jiraCallProperty{marker}},
	}}
	if err := fence.Dispatch(attempt.send); err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return sdkgo.NewMutationRetry[AddCommentOutput](newFailure(addCommentOperationID, sdkgo.FailureAvailability, "Jira dispatch checkpoint or request budget is unavailable"), 0)
		}
		return defectOrSelectUncertain(policy, AddCommentBranchDefect, requested, newFailure(addCommentOperationID, sdkgo.FailureLocalDefect, "Jira dispatch was not allowed"), receipt)
	}
	result := attempt.result
	classification := client.classifyUnkeyedWrite(addCommentOperationID, addCommentFailureSubject, result)
	receipt = client.receipt(session, result.response, "")
	switch classification.outcome {
	case writeRetry:
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[AddCommentOutput](newFailure(addCommentOperationID, sdkgo.FailureAvailability, "Jira retry checkpoint could not be recorded"), classification.retryAfter)
		}
		return sdkgo.NewMutationRetry[AddCommentOutput](classification.failure, classification.retryAfter)
	case writeDefect:
		return defectOrSelectUncertain(policy, AddCommentBranchDefect, requested, classification.failure, receipt)
	case writeUncertain:
		return client.reconcileAddedComment(session, requested, issueIDOrKey, marker, "", policy, classification.failure, receipt)
	case writeNotFound:
		return rejectOrSelectUncertain(fence, policy, addCommentOperationID, AddCommentBranchNotFound, requested, classification.failure, receipt)
	case writeRejected:
		if result.response.statusCode == http.StatusUnauthorized && client.refreshRejectedCredential(session) {
			if err := fence.RecordCanResend(); err != nil {
				return sdkgo.NewMutationRetry[AddCommentOutput](newFailure(addCommentOperationID, sdkgo.FailureAvailability, "Jira credential retry checkpoint could not be recorded"), 0)
			}
			return sdkgo.NewMutationRetry[AddCommentOutput](newFailure(addCommentOperationID, sdkgo.FailureAuthentication, "Jira access token was refreshed for retry"), 0)
		}
		requested.RejectedFieldIDs = classification.rejectedFieldIDs
		return rejectOrSelectUncertain(fence, policy, addCommentOperationID, AddCommentBranchProviderRejected, requested, classification.failure, receipt)
	}
	var comment commentResource
	if json.Unmarshal(result.response.body, &comment) == nil && numericIDPattern.MatchString(comment.ID) {
		// The accepted comment identity is useful after a lost Step result; failure to record it cannot authorize a resend.
		_ = fence.RecordProviderID(comment.ID)
		return addedCommentBranch(requested, comment, receipt)
	}
	return client.reconcileAddedComment(session, requested, issueIDOrKey, marker, "", policy, newFailure(addCommentOperationID, sdkgo.FailureProtocol, "Jira accepted the comment but returned an unusable comment reference"), receipt)
}

func addedCommentBranch(requested AddCommentOutput, comment commentResource, receipt sdkgo.Receipt) sdkgo.MutationAttempt[AddCommentOutput] {
	output := requested
	output.CommentID = comment.ID
	if author := comment.Author.view(); author != nil {
		output.AuthorAccountID = author.AccountID
	}
	// The comment exists either way; an unparseable timestamp only leaves CreatedAt zero.
	if createdAt, err := parseJiraTime(comment.Created); err == nil {
		output.CreatedAt = createdAt
	}
	receipt.ProviderObjectID = comment.ID
	return sdkgo.NewMutationBranch(AddCommentBranchAdded, output, nil, receipt)
}
