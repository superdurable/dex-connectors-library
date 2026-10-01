// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira

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
	Body documentNode `json:"body"`
}

type commentResource struct {
	ID      string           `json:"id"`
	Created string           `json:"created"`
	Author  *accountResource `json:"author"`
}

// Definition returns the immutable connector operation definition.
func (AddCommentOperation) Definition() sdkgo.MutationDefinition { return AddCommentDefinition }

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID.
// Jira accepts no idempotency key, so it is never sent and cannot deduplicate a repeated comment.
func (AddCommentOperation) IdempotencyKey(callID sdkgo.CallID, _ AddCommentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke adds one comment and never resends a request Jira may have received.
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
	session, cancel, sessionErr := client.startSession(call, addCommentOperationID)
	if sessionErr != nil {
		if sessionErr.isRetryable {
			return sdkgo.NewMutationRetry[AddCommentOutput](sessionErr.failure, sessionErr.retryAfter)
		}
		return sdkgo.NewMutationBranch(AddCommentBranchDefect, requested, sessionErr.pointer(), sdkgo.Receipt{})
	}
	defer cancel()
	result := client.exchange(session, jiraRequest{
		method: http.MethodPost, path: issuePath(issueIDOrKey) + "/comment",
		payload: addCommentRequestBody{Body: convertPlainTextToDocument(input.Body)},
	})
	classification := client.classifyUnkeyedWrite(addCommentOperationID, addCommentFailureSubject, result)
	receipt := client.receipt(session, result.response, "")
	switch classification.outcome {
	case writeAccepted:
	case writeRetry:
		return sdkgo.NewMutationRetry[AddCommentOutput](classification.failure, classification.retryAfter)
	case writeDefect:
		return sdkgo.NewMutationBranch(AddCommentBranchDefect, requested, &classification.failure, receipt)
	case writeUncertain:
		return sdkgo.NewMutationUncertain(requested, classification.failure, receipt)
	case writeNotFound:
		return sdkgo.NewMutationBranch(AddCommentBranchNotFound, requested, &classification.failure, receipt)
	default:
		requested.RejectedFieldIDs = classification.rejectedFieldIDs
		return sdkgo.NewMutationBranch(AddCommentBranchProviderRejected, requested, &classification.failure, receipt)
	}
	var comment commentResource
	if err := json.Unmarshal(result.response.body, &comment); err != nil || !numericIDPattern.MatchString(comment.ID) {
		return sdkgo.NewMutationUncertain(requested, newFailure(addCommentOperationID, sdkgo.FailureProtocol, "Jira accepted the comment but returned an unusable comment reference"), receipt)
	}
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
