// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const replyToConversationOperation = "replyToConversation"

// ReplyToConversationInput is one public reply or internal note on a conversation, attributed
// to the connection's staff user.
type ReplyToConversationInput struct {
	// ConversationID is the conversation's slug, such as knock-knock.
	ConversationID string `json:"conversationId"`
	// Text is the reply or note, at most MaxTextBytes. It is required and sent unchanged as
	// Re:amaze's message body, which Re:amaze formats as Markdown.
	Text string `json:"text"`
	// IsInternalNote adds a note only staff see, Re:amaze visibility 1. False adds a public reply,
	// visibility 0, which Re:amaze sends to the customer through the conversation's channel.
	IsInternalNote bool `json:"isInternalNote,omitempty"`
	// ShouldSuppressNotifications asks Re:amaze to send no email or integration notification for
	// the message, Re:amaze's suppress_notifications.
	ShouldSuppressNotifications bool `json:"shouldSuppressNotifications,omitempty"`
	// ShouldSuppressAutoResolve asks Re:amaze not to mark the conversation Done because a staff
	// user wrote, Re:amaze's suppress_autoresolve.
	ShouldSuppressAutoResolve bool `json:"shouldSuppressAutoResolve,omitempty"`
}

// ConversationReply is the message replyToConversation added.
type ConversationReply struct {
	// ConversationID is the conversation the message belongs to.
	ConversationID string `json:"conversationId"`
	// Message is the reply or note as Re:amaze returned it. Its OriginID is the Step's dispatch key.
	Message ConversationMessage `json:"message"`
	// WasAlreadyApplied reports that an earlier attempt of this Step added the message without a
	// confirmed outcome and this attempt found it, so nothing was sent again.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
}

// ReplyToConversationOperation is the replyToConversation Mutation.
type ReplyToConversationOperation struct {
	client *Client
}

type replyRequestWire struct {
	Message replyMessageWire `json:"message"`
}

type replyMessageWire struct {
	Body                        string `json:"body"`
	Visibility                  int    `json:"visibility"`
	OriginID                    string `json:"origin_id"`
	ShouldSuppressNotifications bool   `json:"suppress_notifications,omitempty"`
	ShouldSuppressAutoResolve   bool   `json:"suppress_autoresolve,omitempty"`
}

// Definition returns the immutable connector operation definition.
func (ReplyToConversationOperation) Definition() sdkgo.MutationDefinition {
	return ReplyToConversationDefinition
}

// IdempotencyKey uses the stable connector Call ID. Re:amaze documents origin_id as a unique
// message identifier that helps prevent duplicates, so the key becomes the message's origin_id.
func (ReplyToConversationOperation) IdempotencyKey(callID sdkgo.CallID, _ ReplyToConversationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /conversations/{slug}/messages once per Step execution, after recording a
// dispatch checkpoint. An attempt that finds the checkpoint never sends: it reads the
// conversation's newest messages and reports the one whose origin_id is the Step's key, or
// selects uncertain. Only a 429 or a connection that never opened clears the checkpoint. Dex
// accepts the checkpoint when the Worker writes it to its stream, so a Worker lost before Dex
// stored it can still send twice.
func (operation ReplyToConversationOperation) Invoke(call sdkgo.Call, input ReplyToConversationInput) sdkgo.MutationAttempt[ConversationReply] {
	output := ConversationReply{ConversationID: input.ConversationID}
	if err := validateReplyToConversationInput(input); err != nil {
		return sdkgo.NewMutationBranch(ReplyToConversationBranchDefect, output, reamazeFailurePointer(sdkgo.FailureValidation, replyToConversationOperation, err.Error()), sdkgo.Receipt{})
	}
	isAlreadyDispatched := hasEarlierDispatch(call)
	credentials, err := operation.client.resolveCredentials(call)
	if err != nil {
		return singleDispatchAttemptForCredentials(replyToConversationOperation, ReplyToConversationBranchDefect, output, err,
			isAlreadyDispatched, operation.client.receipt(call, reamazeResponse{}, input.ConversationID))
	}
	if isAlreadyDispatched {
		return operation.reconcileEarlierDispatch(call, credentials, input)
	}
	if !recordSingleDispatch(call) {
		return singleDispatchAttemptNotRecorded[ConversationReply](replyToConversationOperation)
	}
	result := operation.client.exchange(call, credentials, replyToConversationOperation, reamazeRequest{
		method: http.MethodPost, path: conversationPath(input.ConversationID) + "/messages", payload: buildReplyRequest(input, dispatchKey(call)),
	})
	receipt := operation.client.receipt(call, result.response, input.ConversationID)
	if attempt, isTerminal := singleDispatchAttemptForSend[ConversationReply](call, result, receipt, singleDispatchBranches{
		notFound: ReplyToConversationBranchNotFound, providerRejected: ReplyToConversationBranchProviderRejected, defect: ReplyToConversationBranchDefect,
	}); isTerminal {
		return attempt
	}
	message, err := decodeCreatedMessage(result.response.body, input.ConversationID)
	if err != nil {
		return sdkgo.NewMutationUncertain(output, reamazeFailure(sdkgo.FailureProtocol, replyToConversationOperation,
			"Re:amaze accepted the message but returned an invalid message: "+err.Error()), receipt)
	}
	output.Message = message
	return sdkgo.NewMutationBranch(ReplyToConversationBranchReplied, output, nil, receipt)
}

// reconcileEarlierDispatch never sends: it reports an earlier attempt's message found by origin_id, or uncertain.
func (operation ReplyToConversationOperation) reconcileEarlierDispatch(call sdkgo.Call, credentials Credentials, input ReplyToConversationInput) sdkgo.MutationAttempt[ConversationReply] {
	output := ConversationReply{ConversationID: input.ConversationID}
	result := operation.client.exchange(call, credentials, replyToConversationOperation, reamazeRequest{
		method: http.MethodGet, path: conversationPath(input.ConversationID) + "/messages", query: url.Values{"page": {"1"}},
	})
	receipt := operation.client.receipt(call, result.response, input.ConversationID)
	if attempt, isTerminal := singleDispatchAttemptForReconciliationRead[ConversationReply](result, receipt, replyToConversationOperation,
		ReplyToConversationBranchNotFound); isTerminal {
		return attempt
	}
	messages, _, err := decodeMessagePage(result.response.body)
	if err != nil {
		return sdkgo.NewMutationUncertain(output, reamazeFailure(sdkgo.FailureProtocol, replyToConversationOperation,
			"an earlier attempt of this Step sent the message, and the message page read back is invalid: "+err.Error()), receipt)
	}
	key := dispatchKey(call)
	for _, message := range messages {
		if message.OriginID == key {
			output.Message, output.WasAlreadyApplied = message, true
			return sdkgo.NewMutationBranch(ReplyToConversationBranchReplied, output, nil, receipt)
		}
	}
	return sdkgo.NewMutationUncertain(output, reamazeFailure(sdkgo.FailureTransport, replyToConversationOperation, uncertainNotFoundMessage), receipt)
}

// decodeCreatedMessage decodes the message object Re:amaze returns and checks its conversation when named.
func decodeCreatedMessage(body []byte, conversationID string) (ConversationMessage, error) {
	var document struct {
		messageWire
		Conversation *struct {
			Slug string `json:"slug"`
		} `json:"conversation"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		return ConversationMessage{}, errors.New("response is not a message object")
	}
	if document.Conversation != nil && document.Conversation.Slug != "" && document.Conversation.Slug != conversationID {
		return ConversationMessage{}, errors.New("response is for another conversation")
	}
	return decodeMessageWire(document.messageWire)
}

func validateReplyToConversationInput(input ReplyToConversationInput) error {
	if err := validateConversationID("conversationId", input.ConversationID); err != nil {
		return err
	}
	return validateTextInput("text", input.Text)
}

func buildReplyRequest(input ReplyToConversationInput, key string) replyRequestWire {
	visibility := MessageVisibilityRegular
	if input.IsInternalNote {
		visibility = MessageVisibilityInternalNote
	}
	return replyRequestWire{Message: replyMessageWire{
		Body: input.Text, Visibility: int(visibility), OriginID: key,
		ShouldSuppressNotifications: input.ShouldSuppressNotifications, ShouldSuppressAutoResolve: input.ShouldSuppressAutoResolve,
	}}
}
