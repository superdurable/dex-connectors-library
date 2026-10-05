// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	addCommentOperationID    = "addComment"
	addCommentFailureSubject = "comment"
)

// AddCommentInput is one public reply or internal note on a request.
type AddCommentInput struct {
	// IssueIDOrKey is a request key such as ITH-12 or a numeric issue ID.
	IssueIDOrKey string `json:"issueIdOrKey"`
	// Body is the comment's text, at most 32767 characters. Jira Service Management renders wiki markup
	// in it, so characters such as * and _ can format the text; the connector sends it unchanged.
	Body string `json:"body"`
	// IsPublic adds a public reply that Jira Service Management shares with the customer and notifies
	// them of. False adds an internal note that only agents see. Visibility is fixed when it is added.
	IsPublic bool `json:"isPublic,omitempty"`
}

// AddCommentOutput identifies the added comment. On notFound, providerRejected, and uncertain, CommentID
// is empty and the request and visibility are echoed.
type AddCommentOutput struct {
	// IssueIDOrKey echoes the request.
	IssueIDOrKey string `json:"issueIdOrKey"`
	// CommentID is the new comment's ID.
	CommentID string `json:"commentId,omitempty"`
	// IsPublic is the comment's visibility as requested, or as the provider confirmed it on added.
	IsPublic bool `json:"isPublic"`
	// CreatedAt is when the comment was added, or nil when the provider gave no date.
	CreatedAt *time.Time `json:"createdAt,omitempty"`
}

// AddCommentOperation implements the addComment Mutation.
type AddCommentOperation struct{ client *Client }

type addCommentRequestBody struct {
	Body   string `json:"body"`
	Public bool   `json:"public"`
}

// Definition returns the immutable connector operation definition.
func (AddCommentOperation) Definition() sdkgo.MutationDefinition { return AddCommentDefinition }

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID. Jira Service Management
// accepts no idempotency key, so it is never sent; single dispatch comes from a Dex heartbeat checkpoint.
func (AddCommentOperation) IdempotencyKey(callID sdkgo.CallID, _ AddCommentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /rest/servicedeskapi/request/{issueIdOrKey}/comment at most once per Step execution.
// Only a 429 or a connection that never opened is retried; any other unconfirmed outcome selects uncertain.
func (operation AddCommentOperation) Invoke(call sdkgo.Call, input AddCommentInput) sdkgo.MutationAttempt[AddCommentOutput] {
	client := operation.client
	requested := AddCommentOutput{IssueIDOrKey: input.IssueIDOrKey, IsPublic: input.IsPublic}
	issueIDOrKey, err := validateIssueIDOrKey(input.IssueIDOrKey)
	if err != nil {
		return sdkgo.NewMutationBranch(AddCommentBranchDefect, requested, failurePointer(addCommentOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	requested.IssueIDOrKey = issueIDOrKey
	if err := validatePlainText(input.Body, "body", true); err != nil {
		return sdkgo.NewMutationBranch(AddCommentBranchDefect, requested, failurePointer(addCommentOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, sessionErr := client.startSession(call, addCommentOperationID)
	if sessionErr != nil {
		if sessionErr.isRetryable {
			return sdkgo.NewMutationRetry[AddCommentOutput](sessionErr.failure, sessionErr.retryAfter)
		}
		return sdkgo.NewMutationBranch(AddCommentBranchDefect, requested, sessionErr.pointer(), sdkgo.Receipt{})
	}
	defer cancel()
	if attempt, isTerminal := singleDispatchAttemptBeforeSend(claimSingleDispatch(call), addCommentOperationID, requested, client.emptyReceipt(session)); isTerminal {
		return attempt
	}
	result := client.exchange(session, providerRequest{
		method: http.MethodPost, api: serviceDeskAPI, path: requestPath(issueIDOrKey) + "/comment",
		payload: addCommentRequestBody{Body: input.Body, Public: input.IsPublic},
	})
	classification := client.classifyUnkeyedWrite(addCommentOperationID, addCommentFailureSubject, result)
	receipt := client.receipt(session, result.response, issueIDOrKey)
	if attempt, isTerminal := singleDispatchAttemptForWrite(call, classification, requested, receipt, singleDispatchBranches{
		notFound: AddCommentBranchNotFound, providerRejected: AddCommentBranchProviderRejected, defect: AddCommentBranchDefect,
	}); isTerminal {
		return attempt
	}
	var created serviceDeskComment
	if err := json.Unmarshal(result.response.body, &created); err != nil || !isSafeProviderToken(created.ID) {
		return sdkgo.NewMutationUncertain(requested, newFailure(addCommentOperationID, sdkgo.FailureProtocol,
			"Jira Service Management accepted the comment but returned an unusable comment reference"), receipt)
	}
	output := requested
	output.CommentID, output.CreatedAt = created.ID, created.Created.timePointer()
	if created.Public != nil {
		output.IsPublic = *created.Public
	}
	return sdkgo.NewMutationBranch(AddCommentBranchAdded, output, nil, receipt)
}
