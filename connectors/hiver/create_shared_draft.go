// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	createSharedDraftOperation = "createSharedDraft"

	// maximumDraftBodyBytes bounds one draft body; Hiver documents no limit.
	maximumDraftBodyBytes = 65536
)

// smtpMessageIDPattern is a Message-ID header value such as <abc123@mail.gmail.com>.
var smtpMessageIDPattern = regexp.MustCompile(`^<[\x21-\x3B\x3D\x3F-\x7E]{1,994}>$`)

// CreateSharedDraftInput is one reply draft to one message of a conversation. Exactly one of
// HiverMessageID and GmailMessageID is required.
type CreateSharedDraftInput struct {
	// InboxID is the Hiver shared inbox ID, as listInboxes returns it.
	InboxID string `json:"inboxId"`
	// HiverMessageID is the Hiver message ID to reply to, as getConversation returns it.
	HiverMessageID string `json:"hiverMessageId,omitempty"`
	// GmailMessageID is the shared mailbox user's Gmail message ID to reply to, as
	// getConversation returns it, or the message's SMTP Message-ID header such as
	// <abc123@mail.gmail.com>, which Hiver accepts as a fallback.
	GmailMessageID string `json:"gmailMessageId,omitempty"`
	// Body is the reply text Hiver places in the draft. Hiver's documentation does not say
	// whether it renders markup, so send plain text unless a test in your account shows otherwise.
	Body string `json:"body"`
}

// SharedDraft is a reply draft that Hiver users can review, edit, and send from the conversation.
type SharedDraft struct {
	// ID is the Hiver shared draft ID.
	ID string `json:"id"`
	// ConversationID is the Hiver conversation the draft belongs to.
	ConversationID string `json:"conversationId,omitempty"`
	// ReplyToHiverMessageID is the Hiver message the draft replies to.
	ReplyToHiverMessageID string `json:"replyToHiverMessageId,omitempty"`
}

// CreateSharedDraftOperation is the createSharedDraft Mutation.
type CreateSharedDraftOperation struct {
	client *Client
}

type sharedDraftWire struct {
	ConversationID   wireID `json:"id"`
	SharedDraftID    wireID `json:"shared_draft_id"`
	ReplyToMessageID wireID `json:"reply_to_message_id"`
}

// Definition returns the immutable connector operation definition.
func (CreateSharedDraftOperation) Definition() sdkgo.MutationDefinition {
	return CreateSharedDraftDefinition
}

// IdempotencyKey uses the stable connector Call ID. Hiver documents no idempotency key, so the
// key only correlates the Receipt; single dispatch comes from a Dex heartbeat checkpoint instead.
func (CreateSharedDraftOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateSharedDraftInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /v1/inboxes/{inbox_id}/conversations/shared-drafts as multipart/form-data
// unless an earlier attempt of this Step execution recorded the dispatch checkpoint, in which
// case it selects uncertain without sending. Hiver never sends the draft to the customer; a
// Hiver user does. Only a 429, a connection that never opened, or a cancelled wait for a
// request slot is retried; any other unconfirmed outcome selects uncertain without resending.
// Dex may not yet have stored the checkpoint when the draft is sent, so a Worker that loses
// its Dex connection in that instant can create a second draft on the retry.
func (operation CreateSharedDraftOperation) Invoke(call sdkgo.Call, input CreateSharedDraftInput) sdkgo.MutationAttempt[SharedDraft] {
	if err := validateCreateSharedDraftInput(input); err != nil {
		return sdkgo.NewMutationBranch(CreateSharedDraftBranchDefect, SharedDraft{}, hiverFailurePointer(sdkgo.FailureValidation, createSharedDraftOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, createSharedDraftOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateSharedDraftBranchDefect, SharedDraft{}, failure, sdkgo.Receipt{})
	}
	fields := []hiverFormField{{name: "body", value: input.Body}}
	if input.HiverMessageID != "" {
		fields = append(fields, hiverFormField{name: "hiver_message_id", value: input.HiverMessageID})
	} else {
		fields = append(fields, hiverFormField{name: "gmail_message_id", value: input.GmailMessageID})
	}
	result, attempt := sendOnce[SharedDraft](call, operation.client, credentials, createSharedDraftOperation, hiverRequest{
		method: http.MethodPost, path: inboxPath(input.InboxID) + "/conversations/shared-drafts", formFields: fields,
	}, singleDispatchBranches{notFound: CreateSharedDraftBranchNotFound, providerRejected: CreateSharedDraftBranchProviderRejected, defect: CreateSharedDraftBranchDefect})
	if attempt != nil {
		return *attempt
	}
	draft, err := decodeSharedDraft(result.response.body)
	if err != nil {
		return sdkgo.NewMutationUncertain(SharedDraft{}, hiverFailure(sdkgo.FailureProtocol, createSharedDraftOperation,
			"Hiver accepted the draft but returned an invalid shared draft: "+err.Error()), operation.client.receipt(call, result.response, ""))
	}
	return sdkgo.NewMutationBranch(CreateSharedDraftBranchCreated, draft, nil, operation.client.receipt(call, result.response, draft.ID))
}

func validateCreateSharedDraftInput(input CreateSharedDraftInput) error {
	var messageErr error
	switch {
	case (input.HiverMessageID == "") == (input.GmailMessageID == ""):
		messageErr = errors.New("exactly one of hiverMessageId and gmailMessageId is required")
	case input.HiverMessageID != "":
		messageErr = validateHiverID("hiverMessageId", input.HiverMessageID)
	case !gmailIdentifierPattern.MatchString(input.GmailMessageID) && !smtpMessageIDPattern.MatchString(input.GmailMessageID):
		messageErr = errors.New("gmailMessageId must be a Gmail message ID or an SMTP Message-ID such as <abc123@mail.gmail.com>")
	}
	return errors.Join(validateHiverID("inboxId", input.InboxID), messageErr, validateText("body", input.Body, maximumDraftBodyBytes))
}

func decodeSharedDraft(body []byte) (SharedDraft, error) {
	raw, err := decodeSingleData(body)
	if err != nil {
		return SharedDraft{}, err
	}
	var wire sharedDraftWire
	if json.Unmarshal(raw, &wire) != nil || !hiverIDPattern.MatchString(string(wire.SharedDraftID)) {
		return SharedDraft{}, errors.New("shared_draft_id is missing or invalid")
	}
	for _, optionalID := range []wireID{wire.ConversationID, wire.ReplyToMessageID} {
		if optionalID != "" && !hiverIDPattern.MatchString(string(optionalID)) {
			return SharedDraft{}, errors.New("shared draft conversation or message ID is invalid")
		}
	}
	return SharedDraft{ID: string(wire.SharedDraftID), ConversationID: string(wire.ConversationID), ReplyToHiverMessageID: string(wire.ReplyToMessageID)}, nil
}
