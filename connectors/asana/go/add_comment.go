// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	addCommentOperationID    = "addComment"
	addCommentFailureSubject = "comment"
)

// createdStoryFields are the opt_fields of the comment story Asana returns.
var createdStoryFields = []string{"gid", "created_at", "created_by.gid", "created_by.name", "resource_subtype"}

// AddCommentInput describes one comment on one task.
type AddCommentInput struct {
	// TaskID is the task's gid.
	TaskID string `json:"taskId"`
	// Text is the plain-text comment, at most 65536 characters; line breaks are kept and the text is never
	// read as markup.
	Text string `json:"text"`
}

// AddCommentOutput identifies the added comment. On notFound, providerRejected, and uncertain, CommentID is
// empty and TaskID echoes the request.
type AddCommentOutput struct {
	// TaskID echoes the requested task.
	TaskID string `json:"taskId"`
	// CommentID is the new comment story's gid.
	CommentID string `json:"commentId,omitempty"`
	// Author is the user that wrote the comment, the connection's user.
	Author *UserReference `json:"author,omitempty"`
	// CreatedAt is when Asana stored the comment.
	CreatedAt *time.Time `json:"createdAt,omitempty"`
}

// AddCommentOperation implements the addComment Mutation.
type AddCommentOperation struct{ client *Client }

type addCommentFields struct {
	Text string `json:"text"`
}

type storyResource struct {
	GID       string         `json:"gid"`
	CreatedAt string         `json:"created_at"`
	CreatedBy *namedResource `json:"created_by"`
}

// Definition returns the immutable connector operation definition.
func (AddCommentOperation) Definition() sdkgo.MutationDefinition { return AddCommentDefinition }

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID.
// Asana accepts no idempotency key, so it is never sent and cannot deduplicate a repeated comment.
func (AddCommentOperation) IdempotencyKey(callID sdkgo.CallID, _ AddCommentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke adds one comment, retrying only outcomes that permit another POST and reading back a checkpointed story.
func (operation AddCommentOperation) Invoke(call sdkgo.Call, input AddCommentInput) sdkgo.MutationAttempt[AddCommentOutput] {
	client := operation.client
	requested := AddCommentOutput{TaskID: strings.TrimSpace(input.TaskID)}
	taskID, err := validateGID(input.TaskID, "taskId")
	if err == nil {
		err = validatePlainText(input.Text, "text", true)
	}
	if err != nil {
		return sdkgo.NewMutationBranch(AddCommentBranchDefect, requested, failurePointer(addCommentOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	write, savedStoryID, attempt, isFinal := startFencedWrite(client, call, AddCommentDefinition, addCommentOperationID, requested, AddCommentBranchDefect)
	if isFinal {
		return attempt
	}
	session, cancel, startFailure := client.startSession(call, addCommentOperationID)
	if startFailure != nil {
		return sessionFailureAttempt(write, addCommentOperationID, startFailure, requested, AddCommentBranchDefect)
	}
	defer cancel()
	if savedStoryID != "" {
		return operation.readBackAddedComment(session, requested, savedStoryID)
	}
	result, attempt, isFinal := dispatchFencedWrite(write, client, session, asanaRequest{
		method: http.MethodPost, path: joinPath("tasks", taskID, "stories"), query: url.Values{"opt_fields": {strings.Join(createdStoryFields, ",")}},
		payload: dataEnvelope[addCommentFields]{Data: addCommentFields{Text: input.Text}},
	}, addCommentOperationID, requested, AddCommentBranchDefect)
	if isFinal {
		return attempt
	}
	classification := client.classifyUnkeyedWrite(addCommentOperationID, addCommentFailureSubject, result)
	receipt := client.receipt(session, "")
	if attempt, isFinal := attemptForWrite(write, addCommentOperationID, classification, requested, receipt,
		AddCommentBranchNotFound, AddCommentBranchProviderRejected, AddCommentBranchDefect); isFinal {
		return attempt
	}
	story, err := decodeData[storyResource](result.response.body)
	if err != nil || !gidPattern.MatchString(story.GID) {
		return retryAcceptedReadBackOrSelectUncertain(write, addCommentOperationID, acceptedObjectID(result.response.body), requested,
			"Asana accepted the comment but returned an unusable story reference", receipt)
	}
	output := requested
	output.CommentID, output.Author = story.GID, story.CreatedBy.userView()
	// A saved ID makes a replayed attempt read the comment back instead of sending a second one.
	recordAcceptedWrite(write, story.GID)
	// The comment exists either way; an unparseable timestamp only leaves CreatedAt nil.
	if createdAt, err := parseOptionalAsanaTime(&story.CreatedAt); err == nil {
		output.CreatedAt = createdAt
	}
	receipt.ProviderObjectID = story.GID
	return sdkgo.NewMutationBranch(AddCommentBranchAdded, output, nil, receipt)
}

func (operation AddCommentOperation) readBackAddedComment(session *operationSession, requested AddCommentOutput, storyID string) sdkgo.MutationAttempt[AddCommentOutput] {
	client := operation.client
	if !client.canStartRequest(session) {
		return sdkgo.NewMutationRetry[AddCommentOutput](newFailure(addCommentOperationID, sdkgo.FailureTransport, "Asana comment read-back attempt deadline is too close"), 0)
	}
	result := client.exchange(session, asanaRequest{
		method: http.MethodGet, path: joinPath("stories", storyID),
		query: url.Values{"opt_fields": {strings.Join(createdStoryFields, ",")}},
	})
	classification := client.classifyRead(addCommentOperationID, addCommentFailureSubject, result)
	if classification.outcome == readRetry {
		return sdkgo.NewMutationRetry[AddCommentOutput](classification.failure, classification.retryAfter)
	}
	if classification.outcome != readSucceeded {
		return sdkgo.NewMutationUncertain(requested, newFailure(addCommentOperationID, sdkgo.FailureProtocol, "Asana comment could not be confirmed by ID"), client.receipt(session, storyID))
	}
	story, err := decodeData[storyResource](result.response.body)
	if err != nil || story.GID != storyID {
		return sdkgo.NewMutationUncertain(requested, newFailure(addCommentOperationID, sdkgo.FailureProtocol, "Asana comment read-back did not match the checkpointed ID"), client.receipt(session, storyID))
	}
	output := requested
	output.CommentID, output.Author = storyID, story.CreatedBy.userView()
	if createdAt, err := parseOptionalAsanaTime(&story.CreatedAt); err == nil {
		output.CreatedAt = createdAt
	}
	return sdkgo.NewMutationBranch(AddCommentBranchAdded, output, nil, client.receipt(session, storyID))
}
