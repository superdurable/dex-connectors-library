// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	addNoteOperation = "addNote"

	// maximumNoteContentBytes bounds one note; Hiver documents no limit.
	maximumNoteContentBytes = 65536
)

// AddNoteInput is one internal note to add to a conversation.
type AddNoteInput struct {
	// InboxID is the Hiver shared inbox ID, as listInboxes returns it.
	InboxID string `json:"inboxId"`
	// ConversationID is the Hiver conversation ID or the shared mailbox user's Gmail thread ID.
	ConversationID string `json:"conversationId"`
	// Content is the note body: plain text, emoji, or the safe subset of HTML Hiver renders.
	// Only Hiver users see notes; the customer never receives them.
	Content string `json:"content"`
}

// AddNoteOutput is the note Hiver added.
type AddNoteOutput struct {
	// Note is the note Hiver returned.
	Note Note `json:"note"`
}

// Note is one internal note on a conversation.
type Note struct {
	// ID is the Hiver note ID.
	ID string `json:"id"`
	// ConversationID is the Hiver conversation ID Hiver reported for the note.
	ConversationID string `json:"conversationId,omitempty"`
	// AuthorID is the Hiver user the API key acts as.
	AuthorID string `json:"authorId,omitempty"`
	// AuthorEmail is that user's email address.
	AuthorEmail string `json:"authorEmail,omitempty"`
	// CreatedAt is when Hiver created the note, in UTC.
	CreatedAt time.Time `json:"createdAt"`
}

// AddNoteOperation is the addNote Mutation.
type AddNoteOperation struct {
	client *Client
}

type noteWire struct {
	ID             wireID `json:"id"`
	ConversationID wireID `json:"conversation_id"`
	Author         *struct {
		ID    wireID `json:"id"`
		Email string `json:"email"`
	} `json:"author"`
	CreatedAt string `json:"created_at"`
}

// Definition returns the immutable connector operation definition.
func (AddNoteOperation) Definition() sdkgo.MutationDefinition { return AddNoteDefinition }

// IdempotencyKey uses the stable connector Call ID. Hiver documents no idempotency key, so the
// key only correlates the Receipt; the shared single-dispatch fence prevents unsafe resends.
func (AddNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ AddNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /v1/inboxes/{inbox_id}/conversations/{conversation_id}/notes as
// multipart/form-data through the shared fence. A prior possibly sent attempt selects
// uncertain because Hiver has no note list for read-back. Definite 429, 503, and proven
// unsent outcomes permit one retry after a stored checkpoint.
func (operation AddNoteOperation) Invoke(call sdkgo.Call, input AddNoteInput) sdkgo.MutationAttempt[AddNoteOutput] {
	fence, pacerDeadline, priorAttempt := beginSingleDispatch[AddNoteOutput](call, operation.client, addNoteOperation, AddNoteBranchDefect)
	if priorAttempt != nil {
		return *priorAttempt
	}
	if err := validateAddNoteInput(input); err != nil {
		return sdkgo.NewMutationBranch(AddNoteBranchDefect, AddNoteOutput{}, hiverFailurePointer(sdkgo.FailureValidation, addNoteOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, addNoteOperation)
	if failure != nil {
		return retryBeforeSingleDispatch[AddNoteOutput](fence, *failure)
	}
	result, attempt := sendOnce[AddNoteOutput](call, operation.client, credentials, addNoteOperation, hiverRequest{
		method: http.MethodPost, path: conversationPath(input.InboxID, input.ConversationID) + "/notes",
		formFields: []hiverFormField{{name: "content", value: input.Content}},
	}, singleDispatchBranches{notFound: AddNoteBranchNotFound, providerRejected: AddNoteBranchProviderRejected, defect: AddNoteBranchDefect}, fence, pacerDeadline)
	if attempt != nil {
		return *attempt
	}
	note, err := decodeNote(result.response.body)
	if err != nil {
		return sdkgo.NewMutationUncertain(AddNoteOutput{}, hiverFailure(sdkgo.FailureProtocol, addNoteOperation,
			"Hiver accepted the note but returned an invalid note: "+err.Error()), operation.client.receipt(call, result.response, ""))
	}
	// The accepted result is known even if the extra identity checkpoint is lost.
	_ = fence.RecordProviderID(note.ID)
	return sdkgo.NewMutationBranch(AddNoteBranchAdded, AddNoteOutput{Note: note}, nil, operation.client.receipt(call, result.response, note.ID))
}

func validateAddNoteInput(input AddNoteInput) error {
	return errors.Join(validateHiverID("inboxId", input.InboxID), validateHiverID("conversationId", input.ConversationID),
		validateText("content", input.Content, maximumNoteContentBytes))
}

func decodeNote(body []byte) (Note, error) {
	raw, err := decodeSingleData(body)
	if err != nil {
		return Note{}, err
	}
	var wire noteWire
	if json.Unmarshal(raw, &wire) != nil || !hiverIDPattern.MatchString(string(wire.ID)) {
		return Note{}, errors.New("note id is missing or invalid")
	}
	note := Note{ID: string(wire.ID)}
	if wire.ConversationID != "" {
		if !hiverIDPattern.MatchString(string(wire.ConversationID)) {
			return Note{}, errors.New("note conversation_id is invalid")
		}
		note.ConversationID = string(wire.ConversationID)
	}
	if wire.Author != nil {
		if wire.Author.ID != "" && !hiverIDPattern.MatchString(string(wire.Author.ID)) {
			return Note{}, errors.New("note author id is invalid")
		}
		note.AuthorID, note.AuthorEmail = string(wire.Author.ID), wire.Author.Email
	}
	if wire.CreatedAt != "" {
		createdAt, err := time.Parse(time.RFC3339Nano, wire.CreatedAt)
		if err != nil {
			return Note{}, errors.New("note created_at is not an RFC 3339 time")
		}
		note.CreatedAt = createdAt.UTC()
	}
	return note, nil
}
