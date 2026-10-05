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
// only correlates the Receipt; duplicate protection comes from a Dex heartbeat checkpoint instead.
func (AddCommentOperation) IdempotencyKey(callID sdkgo.CallID, _ AddCommentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke records a dispatch checkpoint and then sends POST /task/{task_id}/comment. An attempt that
// finds the checkpoint never sends: it reads the task's newest comments and reports the one comment with
// the same text added since the dispatch, or selects uncertain. Only a 429 or a connection that never
// opened clears the checkpoint for a resend. Dex accepts the checkpoint when the Worker writes it to its
// stream, so a Worker lost before Dex stored it could still send the comment twice.
func (operation AddCommentOperation) Invoke(call sdkgo.Call, input AddCommentInput) sdkgo.MutationAttempt[AddCommentOutput] {
	output := AddCommentOutput{TaskID: input.TaskID}
	if err := validateAddCommentInput(input); err != nil {
		return sdkgo.NewMutationBranch(AddCommentBranchDefect, output, clickupFailurePointer(sdkgo.FailureValidation, addCommentOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, addCommentOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(AddCommentBranchDefect, output, failure, sdkgo.Receipt{})
	}
	var marker writeDispatchMarker
	isMarkerFound, err := call.Context.GetLastHeartbeatValue(&marker)
	if err != nil || isMarkerFound {
		return operation.reconcileEarlierDispatch(call, credentials, input, marker)
	}
	if err := call.Context.RecordHeartbeat(writeDispatchMarker{DispatchedCallID: call.ID, DispatchedAtMilliseconds: operation.client.now().UnixMilli()}); err != nil {
		return sdkgo.NewMutationRetry[AddCommentOutput](clickupFailure(sdkgo.FailureAvailability, addCommentOperation,
			"Dex did not record the dispatch checkpoint; nothing was sent"), 0)
	}
	result := operation.client.exchange(call.Context, credentials, addCommentOperation, clickupRequest{
		method: http.MethodPost, path: taskPath(input.TaskID) + "/comment",
		payload: addCommentWire{CommentText: input.Text, NotifyAll: input.IsCreatorNotified},
	})
	receipt := operation.client.receipt(call, input.TaskID)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeNotSent, exchangeRateLimited:
		releaseWriteDispatch(call)
		return sdkgo.NewMutationRetry[AddCommentOutput](result.failure, result.retryAfter)
	case exchangeUnconfirmed, exchangeInvalid:
		// The checkpoint stays, so the next attempt reads the comments instead of resending.
		return sdkgo.NewMutationRetry[AddCommentOutput](result.failure, result.retryAfter)
	case exchangeNotFound:
		return sdkgo.NewMutationBranch(AddCommentBranchNotFound, output, &result.failure, receipt)
	case exchangeDefect:
		releaseWriteDispatch(call)
		return sdkgo.NewMutationBranch(AddCommentBranchDefect, output, &result.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(AddCommentBranchProviderRejected, output, &result.failure, receipt)
	}
	var created createdCommentWire
	if err := json.Unmarshal(result.response.body, &created); err != nil || !taskIDPattern.MatchString(string(created.ID)) {
		return sdkgo.NewMutationRetry[AddCommentOutput](clickupFailure(sdkgo.FailureProtocol, addCommentOperation,
			"ClickUp accepted the comment but returned no valid comment ID, so the next attempt reads the comments back"), 0)
	}
	output.CommentID, output.CreatedAt = string(created.ID), created.Date.timePointer()
	receipt.Metadata = map[string]string{"commentId": output.CommentID}
	return sdkgo.NewMutationBranch(AddCommentBranchAdded, output, nil, receipt)
}

// reconcileEarlierDispatch never sends: it reports the earlier attempt's comment found on the task, or uncertain.
func (operation AddCommentOperation) reconcileEarlierDispatch(
	call sdkgo.Call, credentials Credentials, input AddCommentInput, marker writeDispatchMarker,
) sdkgo.MutationAttempt[AddCommentOutput] {
	output := AddCommentOutput{TaskID: input.TaskID}
	result := operation.client.exchange(call.Context, credentials, addCommentOperation, clickupRequest{
		method: http.MethodGet, path: taskPath(input.TaskID) + "/comment",
	})
	receipt := operation.client.receipt(call, input.TaskID)
	switch {
	case result.outcome == exchangeSucceeded:
	case result.isRetryableRead():
		return sdkgo.NewMutationRetry[AddCommentOutput](result.failure, result.retryAfter)
	case result.outcome == exchangeNotFound:
		return sdkgo.NewMutationBranch(AddCommentBranchNotFound, output, &result.failure, receipt)
	default:
		return sdkgo.NewMutationUncertain(output, clickupFailure(result.failure.Kind, addCommentOperation,
			"an earlier attempt of this Step sent the comment, and the task's comments could not be read back: "+result.failure.Message), receipt)
	}
	var comments taskCommentsWire
	if err := json.Unmarshal(result.response.body, &comments); err != nil || comments.Comments == nil {
		return sdkgo.NewMutationUncertain(output, clickupFailure(sdkgo.FailureProtocol, addCommentOperation,
			"an earlier attempt of this Step sent the comment, and the comments read back are invalid"), receipt)
	}
	since := time.Time{}
	if marker.DispatchedAtMilliseconds > 0 {
		since = time.UnixMilli(marker.DispatchedAtMilliseconds).Add(-writeReconciliationSkew)
	}
	wanted := commentComparisonText(input.Text)
	matchCount := 0
	for _, comment := range comments.Comments {
		createdAt := comment.Date.timePointer()
		if commentComparisonText(comment.CommentText) != wanted || (!since.IsZero() && (createdAt == nil || createdAt.Before(since))) {
			continue
		}
		matchCount++
		output.CommentID, output.CreatedAt = string(comment.ID), createdAt
	}
	if matchCount != 1 || !taskIDPattern.MatchString(output.CommentID) {
		return sdkgo.NewMutationUncertain(AddCommentOutput{TaskID: input.TaskID}, clickupFailure(sdkgo.FailureTransport, addCommentOperation, fmt.Sprintf(
			"an earlier attempt of this Step sent the comment without a confirmed outcome, and the task shows %d matching comments since, so it is not sent again",
			matchCount)), receipt)
	}
	output.WasAlreadyApplied = true
	receipt.Metadata = map[string]string{"commentId": output.CommentID}
	return sdkgo.NewMutationBranch(AddCommentBranchAdded, output, nil, receipt)
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
