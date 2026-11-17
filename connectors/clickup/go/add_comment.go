// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// MaxCommentBytes bounds a comment's plain text.
	MaxCommentBytes = 32768

	addCommentOperation = "addComment"
)

// AddCommentInput is one plain-text comment to add to a task.
type AddCommentInput struct {
	// TaskID is ClickUp's task ID.
	TaskID string `json:"taskId"`
	// Text is the comment's plain text, at most MaxCommentBytes; ClickUp shows it as written.
	Text string `json:"text"`
	// IsCreatorNotified also notifies the token's user; assignees and watchers are always notified.
	IsCreatorNotified bool `json:"isCreatorNotified,omitempty"`
}

// AddCommentOutput is the comment on the task.
type AddCommentOutput struct {
	// TaskID is the task the comment belongs to.
	TaskID string `json:"taskId"`
	// CommentID is ClickUp's comment ID, or empty on every branch except added.
	CommentID string `json:"commentId,omitempty"`
	// CreatedAt is when ClickUp recorded the comment, when it reported the time.
	CreatedAt *time.Time `json:"createdAt,omitempty"`
	// WasAlreadyApplied reports that an earlier attempt of this Step added the comment without a
	// confirmed outcome and this attempt found it on the task, so nothing was sent again.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
}

// AddCommentOperation is the addComment Mutation.
type AddCommentOperation struct {
	client *Client
}

type addCommentWire struct {
	CommentText string `json:"comment_text"`
	NotifyAll   bool   `json:"notify_all"`
}

type createdCommentWire struct {
	ID   flexibleString   `json:"id"`
	Date unixMilliseconds `json:"date"`
}

type taskCommentsWire struct {
	Comments []struct {
		ID          flexibleString   `json:"id"`
		CommentText string           `json:"comment_text"`
		Date        unixMilliseconds `json:"date"`
	} `json:"comments"`
}

// Definition returns the immutable connector operation definition.
func (AddCommentOperation) Definition() sdkgo.MutationDefinition { return AddCommentDefinition }

// IdempotencyKey uses the stable connector Call ID. ClickUp documents no idempotency key, so the key
// only correlates the Receipt; a Dex heartbeat checkpoint counts each send of the comment instead.
func (AddCommentOperation) IdempotencyKey(callID sdkgo.CallID, _ AddCommentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /task/{task_id}/comment behind the shared single-dispatch fence. An attempt after a
// possible earlier send first reads the task's comments: the one with the saved comment ID when ClickUp
// returned one, otherwise those with the same text added since the Step's first attempt. It reports one
// match, selects uncertain for several, and otherwise resends while possible sends stay within the send
// cap, selecting uncertain at the cap.
func (operation AddCommentOperation) Invoke(call sdkgo.Call, input AddCommentInput) sdkgo.MutationAttempt[AddCommentOutput] {
	output := AddCommentOutput{TaskID: input.TaskID}
	if err := validateAddCommentInput(input); err != nil {
		return sdkgo.NewMutationBranch(AddCommentBranchDefect, output, clickupFailurePointer(sdkgo.FailureValidation, addCommentOperation, err.Error()), sdkgo.Receipt{})
	}
	write, attempt, isDecided := startFencedWrite(call, AddCommentDefinition, addCommentOperation, output, AddCommentBranchDefect)
	if isDecided {
		return attempt
	}
	credentials, failure := operation.client.resolveCredentials(call, addCommentOperation)
	if failure != nil {
		return credentialFailureAttempt(write, addCommentOperation, *failure, output, AddCommentBranchDefect)
	}
	if write.needsReadBack() {
		if attempt, isDecided := operation.reconcileEarlierComment(call, credentials, input, write); isDecided {
			return attempt
		}
	}
	result, attempt, isDecided := dispatchFencedWrite(write, operation.client, credentials, addCommentOperation, clickupRequest{
		method: http.MethodPost, path: taskPath(input.TaskID) + "/comment",
		payload: addCommentWire{CommentText: input.Text, NotifyAll: input.IsCreatorNotified},
	}, output, AddCommentBranchDefect)
	if isDecided {
		return attempt
	}
	receipt := operation.client.receipt(call, input.TaskID)
	if result.classifyWrite() != writeAccepted {
		return attemptForUnacceptedWrite(write, addCommentOperation, result, output, receipt, singleDispatchBranches{
			notFound: AddCommentBranchNotFound, providerRejected: AddCommentBranchProviderRejected, defect: AddCommentBranchDefect,
		})
	}
	var created createdCommentWire
	if err := json.Unmarshal(result.response.body, &created); err != nil || !taskIDPattern.MatchString(string(created.ID)) {
		return sdkgo.NewMutationRetry[AddCommentOutput](clickupFailure(sdkgo.FailureProtocol, addCommentOperation,
			"ClickUp accepted the comment but returned no valid comment ID, so the next attempt reads the comments back"), unknownWriteSettleDelay)
	}
	recordAcceptedWrite(write, string(created.ID))
	output.CommentID, output.CreatedAt = string(created.ID), created.Date.timePointer()
	receipt.Metadata = map[string]string{"commentId": output.CommentID}
	return sdkgo.NewMutationBranch(AddCommentBranchAdded, output, nil, receipt)
}

// reconcileEarlierComment never sends. It decides the attempt unless read-back confirmed nothing and the
// fence still permits a resend.
func (operation AddCommentOperation) reconcileEarlierComment(
	call sdkgo.Call, credentials Credentials, input AddCommentInput, write *fencedWrite,
) (sdkgo.MutationAttempt[AddCommentOutput], bool) {
	output := AddCommentOutput{TaskID: input.TaskID}
	result := operation.client.exchange(call.Context, credentials, addCommentOperation, clickupRequest{
		method: http.MethodGet, path: taskPath(input.TaskID) + "/comment",
	})
	receipt := operation.client.receipt(call, input.TaskID)
	switch {
	case result.outcome == exchangeSucceeded:
	case result.isRetryableRead():
		return sdkgo.NewMutationRetry[AddCommentOutput](result.failure, result.retryAfter), true
	case result.outcome == exchangeNotFound:
		return sdkgo.NewMutationBranch(AddCommentBranchNotFound, output, &result.failure, receipt), true
	default:
		return commentReadBackMissing(write, output, receipt, result.failure)
	}
	var comments taskCommentsWire
	if err := json.Unmarshal(result.response.body, &comments); err != nil || comments.Comments == nil {
		return commentReadBackMissing(write, output, receipt, clickupFailure(sdkgo.FailureProtocol, addCommentOperation, "the comments read back are invalid"))
	}
	providerID := write.decision.Marker.ProviderID
	since := readBackCreatedSince(call)
	wanted := commentComparisonText(input.Text)
	matchCount := 0
	for _, comment := range comments.Comments {
		createdAt := comment.Date.timePointer()
		isMatch := string(comment.ID) == providerID
		if providerID == "" {
			isMatch = commentComparisonText(comment.CommentText) == wanted && (since.IsZero() || (createdAt != nil && !createdAt.Before(since)))
		}
		if isMatch {
			matchCount++
			output.CommentID, output.CreatedAt = string(comment.ID), createdAt
		}
	}
	switch {
	case matchCount > 1:
		return sdkgo.NewMutationUncertain(AddCommentOutput{TaskID: input.TaskID}, clickupFailure(sdkgo.FailureTransport, addCommentOperation, fmt.Sprintf(
			"an earlier attempt of this Step sent the comment without a confirmed outcome, and the task shows %d matching comments since, so it is not sent again",
			matchCount)), receipt), true
	case matchCount == 0 || !taskIDPattern.MatchString(output.CommentID):
		return commentReadBackMissing(write, AddCommentOutput{TaskID: input.TaskID}, receipt,
			clickupFailure(sdkgo.FailureTransport, addCommentOperation, "the task shows no matching comment"))
	}
	output.WasAlreadyApplied = true
	receipt.Metadata = map[string]string{"commentId": output.CommentID}
	return sdkgo.NewMutationBranch(AddCommentBranchAdded, output, nil, receipt), true
}

// commentReadBackMissing permits a resend when the fence allows one, and otherwise selects uncertain.
func commentReadBackMissing(write *fencedWrite, output AddCommentOutput, receipt sdkgo.Receipt, failure sdkgo.Failure) (sdkgo.MutationAttempt[AddCommentOutput], bool) {
	if write.canResendAfterMissingReadBack() {
		return sdkgo.MutationAttempt[AddCommentOutput]{}, false
	}
	return sdkgo.NewMutationUncertain(output, clickupFailure(failure.Kind, addCommentOperation,
		"earlier attempts of this Step sent the comment up to the send cap or were accepted, and read-back cannot confirm the comment, so it is not sent again: "+failure.Message), receipt), true
}

func validateAddCommentInput(input AddCommentInput) error {
	if err := validateTaskID("taskId", input.TaskID); err != nil {
		return err
	}
	if strings.TrimSpace(input.Text) == "" {
		return errors.New("text is required")
	}
	if len(input.Text) > MaxCommentBytes || !utf8.ValidString(input.Text) {
		return fmt.Errorf("text must be valid UTF-8 of at most %d bytes", MaxCommentBytes)
	}
	return nil
}

// commentComparisonText reduces text to its words, so ClickUp's line-break handling does not matter.
func commentComparisonText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
