// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// MaxReplyTextBytes bounds the text of one reply or comment; it is the connector's limit, not Front's.
	MaxReplyTextBytes = 64 << 10

	// replyDeadlineMargin leaves time to return uncertain before the Execute deadline.
	replyDeadlineMargin = 3 * time.Second
	// minimumReplyRequestTime is the least request time worth sending the reply with.
	minimumReplyRequestTime = 5 * time.Second
)

var messageUIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)

// ReplyToConversationInput is one reply to the customer or one internal comment.
type ReplyToConversationInput struct {
	// ConversationID is the Front conversation ID, such as cnv_55c8c149.
	ConversationID string `json:"conversationId"`
	// Text is the plain text to send, required and at most MaxReplyTextBytes. A reply sends it as text and
	// as an HTML body with one paragraph per blank-line-separated block; a comment sends it unchanged, and
	// Front renders Markdown in comments.
	Text string `json:"text"`
	// IsInternalNote adds a comment that only teammates see, through POST /conversations/{id}/comments.
	// False sends a reply that Front delivers to the conversation's recipients, through
	// POST /conversations/{id}/messages.
	IsInternalNote bool `json:"isInternalNote,omitempty"`
	// AuthorID is the teammate the reply is sent on behalf of, or who writes the comment, such as tea_2thf.
	// Blank lets Front choose: a comment is then written as the API token.
	AuthorID string `json:"authorId,omitempty"`
	// ShouldArchive archives the conversation when the reply is sent. False keeps it open, unlike Front's
	// own default; a comment must leave it false.
	ShouldArchive bool `json:"shouldArchive,omitempty"`
}

// ConversationReply is the reply or comment replyToConversation added.
type ConversationReply struct {
	// ConversationID is the conversation the reply or comment belongs to.
	ConversationID string `json:"conversationId"`
	// IsInternalNote reports that a comment was added rather than a reply sent.
	IsInternalNote bool `json:"isInternalNote,omitempty"`
	// MessageUID is Front's message_uid for an accepted reply; GET /messages/alt:uid:{uid} reads the message
	// once Front has created it. Empty for a comment.
	MessageUID string `json:"messageUid,omitempty"`
	// CommentID is the new comment's ID, such as com_1ywg3f2; empty for a reply.
	CommentID string `json:"commentId,omitempty"`
	// AuthorID is the requested author, or empty.
	AuthorID string `json:"authorId,omitempty"`
}

// ReplyToConversationOperation implements the replyToConversation Mutation. Build it with
// Client.ReplyToConversation.
type ReplyToConversationOperation struct{ client *Client }

// replyDispatchCheckpoint is recorded before the request leaves; RecordHeartbeat does not wait for Dex to persist it.
type replyDispatchCheckpoint struct {
	IsDispatched bool `json:"isFrontReplyDispatched"`
}

type replyMessageWire struct {
	Body     string `json:"body"`
	Text     string `json:"text"`
	AuthorID string `json:"author_id,omitempty"`
	Options  struct {
		Archive bool `json:"archive"`
	} `json:"options"`
}

type commentWire struct {
	Body     string `json:"body"`
	AuthorID string `json:"author_id,omitempty"`
}

// Definition returns the immutable replyToConversation operation definition.
func (ReplyToConversationOperation) Definition() sdkgo.MutationDefinition {
	return ReplyToConversationDefinition
}

// IdempotencyKey returns the call ID, recorded in the Receipt only. Front documents no idempotency key,
// and messages and comments carry no client-supplied ID a later attempt could find, which is why the
// operation relies on a Dex dispatch checkpoint instead. The checkpoint narrows but does not close the
// duplicate window: Dex accepts it when the Worker writes it to its stream, so a Worker lost before Dex
// stores it can still send the reply or comment twice.
func (ReplyToConversationOperation) IdempotencyKey(callID sdkgo.CallID, _ ReplyToConversationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke records a Dex heartbeat checkpoint and then sends the reply or comment once. A later attempt of
// the same Step execution that finds the checkpoint selects uncertain without sending. Only a 429 or a
// connection that provably never opened returns Retry and clears the checkpoint. A 408, 5xx, timeout,
// or any other transport failure selects uncertain. The request must answer three seconds before the
// attempt's deadline, so a slow Front selects uncertain rather than a Dex retry.
func (operation ReplyToConversationOperation) Invoke(call sdkgo.Call, input ReplyToConversationInput) sdkgo.MutationAttempt[ConversationReply] {
	attemptStart := time.Now()
	operationID := ReplyToConversationDefinition.Operation.OperationID
	client := operation.client
	output := ConversationReply{ConversationID: input.ConversationID, IsInternalNote: input.IsInternalNote, AuthorID: input.AuthorID}
	if err := validateReplyToConversationInput(input); err != nil {
		return sdkgo.NewMutationBranch(ReplyToConversationBranchDefect, output, frontFailurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	if hasEarlierReplyDispatch(call) {
		return sdkgo.NewMutationUncertain(output, frontFailure(sdkgo.FailureTransport, operationID,
			"an earlier attempt of this Step may have sent the reply or comment, so it is not sent again"), client.receipt(call, input.ConversationID))
	}
	credentials, failure := client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(ReplyToConversationBranchDefect, output, failure, sdkgo.Receipt{})
	}
	requestContext, cancel, isStartable := newReplyRequestContext(call.Context, attemptStart)
	defer cancel()
	if !isStartable {
		return sdkgo.NewMutationRetry[ConversationReply](frontFailure(sdkgo.FailureAvailability, operationID,
			"too little of the Execute timeout remained to send the reply; nothing was sent"), 0)
	}
	if err := call.Context.RecordHeartbeat(replyDispatchCheckpoint{IsDispatched: true}); err != nil {
		return sdkgo.NewMutationRetry[ConversationReply](frontFailure(sdkgo.FailureAvailability, operationID,
			"Dex did not record the dispatch checkpoint, so nothing was sent to Front"), 0)
	}
	result := client.exchange(requestContext, credentials, operationID, buildReplyRequest(input))
	return client.classifyReplyExchange(call, output, result)
}

// classifyReplyExchange maps the sent request: only a provable non-application is retried.
func (client *Client) classifyReplyExchange(call sdkgo.Call, output ConversationReply, result frontExchange) sdkgo.MutationAttempt[ConversationReply] {
	receipt := client.receipt(call, output.ConversationID)
	if result.response.statusCode >= 200 && result.response.statusCode < 300 {
		// Front created the comment or accepted the reply; an unreadable answer only loses its ID.
		output.MessageUID, output.CommentID = decodeReplyIdentity(result.response.body, output.IsInternalNote)
		receipt.ProviderObjectID = cmp.Or(output.CommentID, output.MessageUID, output.ConversationID)
		return sdkgo.NewMutationBranch(ReplyToConversationBranchReplied, output, nil, receipt)
	}
	switch result.outcome {
	case exchangeDefect:
		clearReplyDispatch(call)
		return sdkgo.NewMutationBranch(ReplyToConversationBranchDefect, output, &result.failure, sdkgo.Receipt{})
	case exchangeNotSent, exchangeRateLimited:
		clearReplyDispatch(call)
		return sdkgo.NewMutationRetry[ConversationReply](result.failure, result.retryAfter)
	case exchangeNotFound:
		return sdkgo.NewMutationBranch(ReplyToConversationBranchNotFound, output, &result.failure, receipt)
	case exchangeMerged:
		failure := result.failure
		if mergedInto := mergedConversationID(result.response.header.Get("Location")); mergedInto != "" {
			failure.Message = fmt.Sprintf("the conversation was merged into %s; reply to that conversation", mergedInto)
		}
		return sdkgo.NewMutationBranch(ReplyToConversationBranchNotFound, output, &failure, receipt)
	case exchangeRejected:
		return sdkgo.NewMutationBranch(ReplyToConversationBranchProviderRejected, output, &result.failure, receipt)
	default:
		failure := result.failure
		failure.Message += "; the reply or comment may exist"
		return sdkgo.NewMutationUncertain(output, failure, receipt)
	}
}

func buildReplyRequest(input ReplyToConversationInput) frontRequest {
	conversationPath := "/conversations/" + input.ConversationID
	if input.IsInternalNote {
		return frontRequest{method: http.MethodPost, path: conversationPath + "/comments", payload: commentWire{Body: input.Text, AuthorID: input.AuthorID}}
	}
	payload := replyMessageWire{Body: BuildReplyHTML(input.Text), Text: input.Text, AuthorID: input.AuthorID}
	payload.Options.Archive = input.ShouldArchive
	return frontRequest{method: http.MethodPost, path: conversationPath + "/messages", payload: payload}
}

// BuildReplyHTML returns the HTML body a reply sends for text: each block separated by a blank line
// becomes one escaped paragraph, and each remaining line break becomes <br>.
func BuildReplyHTML(text string) string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	var paragraphs []string
	for _, block := range strings.Split(normalized, "\n\n") {
		if block = strings.Trim(block, "\n"); strings.TrimSpace(block) != "" {
			paragraphs = append(paragraphs, "<p>"+strings.ReplaceAll(html.EscapeString(block), "\n", "<br>")+"</p>")
		}
	}
	return strings.Join(paragraphs, "")
}

// decodeReplyIdentity reads the accepted reply's message_uid or the new comment's ID, or empty values.
func decodeReplyIdentity(body []byte, isInternalNote bool) (string, string) {
	var answer struct {
		ID         string `json:"id"`
		MessageUID string `json:"message_uid"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return "", ""
	}
	if isInternalNote {
		if commentIDPattern.MatchString(answer.ID) {
			return "", answer.ID
		}
		return "", ""
	}
	if messageUIDPattern.MatchString(answer.MessageUID) {
		return answer.MessageUID, ""
	}
	return "", ""
}

func validateReplyToConversationInput(input ReplyToConversationInput) error {
	if err := validateResourceID("conversationId", input.ConversationID, conversationIDPattern, "cnv_55c8c149"); err != nil {
		return err
	}
	switch {
	case strings.TrimSpace(input.Text) == "":
		return errors.New("text is required")
	case len(input.Text) > MaxReplyTextBytes:
		return fmt.Errorf("text is longer than %d bytes", MaxReplyTextBytes)
	case input.IsInternalNote && input.ShouldArchive:
		return errors.New("shouldArchive applies only to a reply, not to an internal comment")
	case input.AuthorID != "":
		return validateResourceID("authorId", input.AuthorID, teammateIDPattern, "tea_2thf")
	}
	return nil
}

// newReplyRequestContext ends the request before the attempt's deadline; false means too little time is left.
func newReplyRequestContext(stepContext context.Context, attemptStart time.Time) (context.Context, context.CancelFunc, bool) {
	if stepContext == nil {
		stepContext = context.Background()
	}
	deadline := attemptStart.Add(ReplyToConversationDefinition.StepDefaults.ExecuteMethodTimeout)
	if contextDeadline, hasDeadline := stepContext.Deadline(); hasDeadline && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	requestDeadline := deadline.Add(-replyDeadlineMargin)
	if time.Until(requestDeadline) < minimumReplyRequestTime {
		return stepContext, func() {}, false
	}
	requestContext, cancel := context.WithDeadline(stepContext, requestDeadline)
	return requestContext, cancel, true
}

// hasEarlierReplyDispatch reports an earlier attempt's checkpoint; an unreadable one counts, avoiding a duplicate.
func hasEarlierReplyDispatch(call sdkgo.Call) bool {
	var checkpoint replyDispatchCheckpoint
	isFound, err := call.Context.GetLastHeartbeatValue(&checkpoint)
	return err != nil || (isFound && checkpoint.IsDispatched)
}

// clearReplyDispatch removes the checkpoint after Front provably added nothing.
func clearReplyDispatch(call sdkgo.Call) {
	// A lost clear leaves the checkpoint set, so the next attempt reports uncertain instead of sending twice.
	_ = call.Context.RecordHeartbeat(nil)
}
