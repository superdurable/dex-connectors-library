// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const replyToMessageOperationID = "replyToMessage"

// ReplyToMessageInput is one plain-text reply to a message in the mailbox.
type ReplyToMessageInput struct {
	// MessageID is the id of the message to answer, from searchMessages or getMessage.
	MessageID string `json:"messageId"`
	// Text is the reply, at most MaxSendTextBytes; Outlook places it above the quoted original.
	Text string `json:"text"`
	// IsReplyAll also addresses the original To and Cc recipients, as Outlook's Reply all does.
	IsReplyAll bool `json:"isReplyAll,omitempty"`
}

// ReplyToMessageOperation implements the replyToMessage Mutation.
type ReplyToMessageOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (ReplyToMessageOperation) Definition() sdkgo.MutationDefinition { return ReplyToMessageDefinition }

// IdempotencyKey uses the stable connector Call ID; it forms the reply draft's IdempotencyMarker.
func (ReplyToMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ ReplyToMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates Outlook's reply draft, which threads and quotes the original, marks it with the Step's
// marker, and sends it at most once like SendMessage.
func (operation ReplyToMessageOperation) Invoke(call sdkgo.Call, input ReplyToMessageInput) sdkgo.MutationAttempt[SentMessage] {
	input.MessageID = strings.TrimSpace(input.MessageID)
	if err := validateGraphID("messageId", input.MessageID); err != nil {
		return sdkgo.NewMutationBranch(ReplyToMessageBranchDefect, SentMessage{}, graphFailurePointer(sdkgo.FailureValidation, replyToMessageOperationID, err.Error()), sdkgo.Receipt{})
	}
	if err := validateSendText(input.Text); err != nil {
		return sdkgo.NewMutationBranch(ReplyToMessageBranchDefect, SentMessage{}, graphFailurePointer(sdkgo.FailureValidation, replyToMessageOperationID, err.Error()), sdkgo.Receipt{})
	}
	return operation.client.sendDraftOnce(call, replyToMessageOperationID, replyDraftCreator{input: input}, draftSendBranches{
		sent: ReplyToMessageBranchSent, notFound: ReplyToMessageBranchNotFound,
		providerRejected: ReplyToMessageBranchProviderRejected, defect: ReplyToMessageBranchDefect,
	})
}

// replyDraftCreator creates the reply draft with createReply or createReplyAll, which take no marker.
type replyDraftCreator struct {
	input ReplyToMessageInput
}

func (creator replyDraftCreator) createDraft(session *graphSession, _ string) (graphMessageWire, graphExchange) {
	action := "/createReply"
	if creator.input.IsReplyAll {
		action = "/createReplyAll"
	}
	result := session.exchange(graphRequest{
		method: http.MethodPost, path: "/messages/" + url.PathEscape(creator.input.MessageID) + action,
		payload: map[string]string{"comment": creator.input.Text},
	})
	if result.outcome == graphInvalid {
		return graphMessageWire{}, unusableReplyDraftAnswer(result, session.operation, "its answer is unusable")
	}
	if result.outcome != graphSucceeded {
		return graphMessageWire{}, result
	}
	draft, err := decodeGraphMessage(result.body)
	if err != nil {
		return graphMessageWire{}, unusableReplyDraftAnswer(result, session.operation, "its answer has no usable draft")
	}
	if len(draft.recipientAddresses()) == 0 {
		return graphMessageWire{}, unusableReplyDraftAnswer(result, session.operation, "the original has no address to reply to")
	}
	return draft, result
}

func (replyDraftCreator) isMarkedOnCreation() bool { return false }

// unusableReplyDraftAnswer refuses instead of retrying: a retry would create another unmarked reply draft.
func unusableReplyDraftAnswer(result graphExchange, operation string, reason string) graphExchange {
	result.outcome = graphRejected
	result.failure = graphFailure(sdkgo.FailureProtocol, operation,
		"Microsoft Graph created a reply draft but "+reason+"; the unsent draft stays in Drafts")
	return result
}
