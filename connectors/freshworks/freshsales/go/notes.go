// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshsales

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// NoteMetadata contains a note identity and text.
type NoteMetadata struct {
	ID          int64  `json:"id"`
	Description string `json:"description"`
}

// CreateNoteInput attaches text to a contact, sales account, or deal.
type CreateNoteInput struct {
	Description string `json:"description"`
	TargetType  string `json:"targetType"`
	TargetID    string `json:"targetId"`
}

// UpdateNoteInput replaces text on an existing note.
type UpdateNoteInput struct {
	NoteID      string `json:"noteId"`
	Description string `json:"description"`
}

// DeleteNoteInput identifies a note to delete.
type DeleteNoteInput struct {
	NoteID string `json:"noteId"`
}

// CreateNoteOperation creates notes with bounded repeated sends.
type CreateNoteOperation struct{ client *Client }

// UpdateNoteOperation replaces note text.
type UpdateNoteOperation struct{ client *Client }

// DeleteNoteOperation deletes a note.
type DeleteNoteOperation struct{ client *Client }

// CreateNote binds note creation to this client.
func (client *Client) CreateNote() CreateNoteOperation { return CreateNoteOperation{client} }

// UpdateNote binds note replacement to this client.
func (client *Client) UpdateNote() UpdateNoteOperation { return UpdateNoteOperation{client} }

// DeleteNote binds note deletion to this client.
func (client *Client) DeleteNote() DeleteNoteOperation { return DeleteNoteOperation{client} }

// Definition returns the manifest-generated operation contract.
func (CreateNoteOperation) Definition() sdkgo.MutationDefinition { return CreateNoteDefinition }

// Definition returns the manifest-generated operation contract.
func (UpdateNoteOperation) Definition() sdkgo.MutationDefinition { return UpdateNoteDefinition }

// Definition returns the manifest-generated operation contract.
func (DeleteNoteOperation) Definition() sdkgo.MutationDefinition { return DeleteNoteDefinition }

// IdempotencyKey correlates a call; Freshsales does not deduplicate note creation.
func (CreateNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates a call; note replacement safely repeats.
func (UpdateNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates a call; note deletion safely repeats.
func (DeleteNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a note using the limited-send Fence.
func (operation CreateNoteOperation) Invoke(call sdkgo.Call, input CreateNoteInput) sdkgo.MutationAttempt[NoteMetadata] {
	if !validNoteDescription(input.Description) || !validTargetType(input.TargetType) || !isPositiveContactID(input.TargetID) {
		return createBranch[NoteMetadata]("createNote", CreateNoteBranchDefect, sdkgo.FailureValidation, "note text or target is invalid")
	}
	targetID, _ := strconv.ParseInt(input.TargetID, 10, 64)
	body, err := json.Marshal(map[string]any{"note": map[string]any{"description": input.Description, "targetable_type": input.TargetType, "targetable_id": targetID}})
	if err != nil {
		return createBranch[NoteMetadata]("createNote", CreateNoteBranchDefect, sdkgo.FailureLocalDefect, "note request cannot be encoded")
	}
	return invokeLimitedCreate(operation.client, call, "createNote", "/notes", body, CreateNoteDefinition.StepDefaults.ExecuteMethodTimeout, createBranches{CreateNoteBranchCreated, CreateNoteBranchProviderRejected, CreateNoteBranchDefect}, decodeNote, func(note NoteMetadata) string { return strconv.FormatInt(note.ID, 10) }, nil)
}

// Invoke replaces note text; an unconfirmed response is retried safely.
func (operation UpdateNoteOperation) Invoke(call sdkgo.Call, input UpdateNoteInput) sdkgo.MutationAttempt[NoteMetadata] {
	if !isPositiveContactID(input.NoteID) || !validNoteDescription(input.Description) {
		return createBranch[NoteMetadata]("updateNote", UpdateNoteBranchDefect, sdkgo.FailureValidation, "note ID or text is invalid")
	}
	body, status, delay, err := operation.client.requestJSON(call, call.Context, http.MethodPut, "/notes/"+input.NoteID, map[string]any{"note": map[string]string{"description": input.Description}})
	switch {
	case errors.Is(err, errCredential):
		return createBranch[NoteMetadata]("updateNote", UpdateNoteBranchDefect, sdkgo.FailureAuthentication, "connection credentials unavailable or invalid")
	case status == http.StatusTooManyRequests:
		return createRetry[NoteMetadata]("updateNote", sdkgo.FailureRateLimit, "Freshsales rate limited note update", delay)
	case status == http.StatusRequestTimeout || status >= 500 || errors.Is(err, errRequest):
		return createRetry[NoteMetadata]("updateNote", sdkgo.FailureAvailability, "note update outcome unconfirmed", delay)
	case status == http.StatusNotFound:
		return createBranch[NoteMetadata]("updateNote", UpdateNoteBranchNotFound, sdkgo.FailureProviderRejection, "note does not exist")
	case status != http.StatusOK:
		return createBranch[NoteMetadata]("updateNote", UpdateNoteBranchProviderRejected, sdkgo.FailureProviderRejection, "Freshsales rejected HTTP "+strconv.Itoa(status))
	case err != nil:
		return createRetry[NoteMetadata]("updateNote", sdkgo.FailureTransport, "note update response interrupted", 0)
	}
	note, decodeErr := decodeNote(body)
	if decodeErr != nil || strconv.FormatInt(note.ID, 10) != input.NoteID || note.Description != input.Description {
		return createRetry[NoteMetadata]("updateNote", sdkgo.FailureProtocol, "note update answer unconfirmed", 0)
	}
	return sdkgo.NewMutationBranch(UpdateNoteBranchUpdated, note, nil, createReceipt(call, input.NoteID))
}

// Invoke deletes a note by ID.
func (operation DeleteNoteOperation) Invoke(call sdkgo.Call, input DeleteNoteInput) sdkgo.MutationAttempt[DeletedCRMRecord] {
	if !isPositiveContactID(input.NoteID) {
		return deletedNamedBranch("deleteNote", DeleteNoteBranchDefect, sdkgo.FailureValidation, "note ID is invalid")
	}
	id, _ := strconv.ParseInt(input.NoteID, 10, 64)
	return invokeNamedDelete(operation.client, call, "deleteNote", "/notes/"+input.NoteID, id, DeleteNoteBranchDeleted, DeleteNoteBranchProviderRejected, DeleteNoteBranchDefect)
}

func validNoteDescription(description string) bool {
	return strings.TrimSpace(description) != "" && len(description) <= 10000
}
func validTargetType(targetType string) bool {
	return targetType == "Contact" || targetType == "SalesAccount" || targetType == "Deal"
}
func decodeNote(body []byte) (NoteMetadata, error) {
	var envelope struct {
		Note NoteMetadata `json:"note"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return NoteMetadata{}, err
	}
	if envelope.Note.ID <= 0 {
		return NoteMetadata{}, errors.New("note ID missing")
	}
	return envelope.Note, nil
}
