// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package close

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// Note identifies a plaintext Close note activity on a lead.
type Note struct {
	// ID is the provider-assigned activity identifier.
	ID string `json:"id"`
	// LeadID identifies the parent lead.
	LeadID string `json:"leadId"`
	// Note contains the plaintext body.
	Note string `json:"note"`
	// Title is the optional note title.
	Title string `json:"title"`
	// Pinned reports whether the note is pinned.
	Pinned bool `json:"pinned"`
}

// NotePage contains a bounded page of note activities.
type NotePage struct {
	// Notes contains the returned notes.
	Notes []Note `json:"notes"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"hasMore"`
}

// ListNotesInput selects a page of notes on one lead.
type ListNotesInput struct {
	// LeadID identifies the parent lead.
	LeadID string `json:"leadId"`
	// Skip is the zero-based offset.
	Skip int `json:"skip,omitempty"`
	// Limit caps notes; zero uses 100.
	Limit int `json:"limit,omitempty"`
}

// GetNoteInput identifies one Close note activity.
type GetNoteInput struct {
	// NoteID is the Close acti_ identifier.
	NoteID string `json:"noteId"`
}

// CreateNoteInput supplies a plaintext note on an existing lead.
type CreateNoteInput struct {
	// LeadID identifies the parent lead.
	LeadID string `json:"leadId"`
	// Note is the nonempty plaintext body.
	Note string `json:"note"`
	// Title is an optional display title.
	Title string `json:"title,omitempty"`
	// Pinned creates a pinned note when true.
	Pinned bool `json:"pinned,omitempty"`
}

// UpdateNoteInput replaces selected note fields.
type UpdateNoteInput struct {
	// NoteID identifies the note to change.
	NoteID string `json:"noteId"`
	// Note replaces the plaintext body when present.
	Note *string `json:"note,omitempty"`
	// Title replaces the title when present; empty clears it.
	Title *string `json:"title,omitempty"`
	// Pinned replaces the pinned state when present.
	Pinned *bool `json:"pinned,omitempty"`
}

// DeleteNoteInput identifies a note to remove.
type DeleteNoteInput struct {
	// NoteID identifies the note to remove.
	NoteID string `json:"noteId"`
}

// DeletedNote confirms that a note is absent.
type DeletedNote struct {
	// NoteID identifies the absent note.
	NoteID string `json:"noteId"`
}

// ListNotesOperation reads notes on one lead.
type ListNotesOperation struct{ client *Client }

// ListNotes binds this Query to the client.
func (client *Client) ListNotes() ListNotesOperation { return ListNotesOperation{client} }

// Definition returns the generated listNotes contract.
func (ListNotesOperation) Definition() sdkgo.QueryDefinition { return ListNotesDefinition }

// Invoke validates pagination and classifies the Close response.
func (operation ListNotesOperation) Invoke(call sdkgo.Call, input ListNotesInput) sdkgo.QueryAttempt[NotePage] {
	if !validLeadID(input.LeadID) || input.Skip < 0 || input.Limit < 0 || input.Limit > 100 {
		return failedBranch[NotePage]("listNotes", ListNotesBranchDefect, sdkgo.FailureValidation, "leadId, skip, or limit is invalid")
	}
	limit := input.Limit
	if limit == 0 {
		limit = 100
	}
	query := url.Values{"lead_id": {input.LeadID}, "_skip": {strconv.Itoa(input.Skip)}, "_limit": {strconv.Itoa(limit)}}
	return invokeGet(operation.client, call, "listNotes", "/activity/note/?"+query.Encode(), ListNotesBranchListed, ListNotesBranchProviderRejected, decodeNotePage)
}

// GetNoteOperation reads one note activity.
type GetNoteOperation struct{ client *Client }

// GetNote binds this Query to the client.
func (client *Client) GetNote() GetNoteOperation { return GetNoteOperation{client} }

// Definition returns the generated getNote contract.
func (GetNoteOperation) Definition() sdkgo.QueryDefinition { return GetNoteDefinition }

// Invoke validates the ID and classifies the Close response.
func (operation GetNoteOperation) Invoke(call sdkgo.Call, input GetNoteInput) sdkgo.QueryAttempt[Note] {
	if !validNoteID(input.NoteID) {
		return failedBranch[Note]("getNote", GetNoteBranchDefect, sdkgo.FailureValidation, "noteId is invalid")
	}
	return invokeGet(operation.client, call, "getNote", "/activity/note/"+input.NoteID+"/", GetNoteBranchFound, GetNoteBranchNotFound, decodeNote)
}

// CreateNoteOperation creates a note with bounded possible sends.
type CreateNoteOperation struct{ client *Client }

// CreateNote binds this Mutation to the client.
func (client *Client) CreateNote() CreateNoteOperation { return CreateNoteOperation{client} }

// Definition returns the generated createNote contract.
func (CreateNoteOperation) Definition() sdkgo.MutationDefinition { return CreateNoteDefinition }

// IdempotencyKey correlates attempts; Close does not deduplicate this key.
func (CreateNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a plaintext note and reconciles a known provider ID before resending.
func (operation CreateNoteOperation) Invoke(call sdkgo.Call, input CreateNoteInput) sdkgo.MutationAttempt[Note] {
	if !validLeadID(input.LeadID) || strings.TrimSpace(input.Note) == "" || len(input.Note) > 100000 || len(input.Title) > 1000 {
		return closeMutationBranch[Note]("createNote", CreateNoteBranchDefect, sdkgo.FailureValidation, "leadId, note, or title is invalid")
	}
	requestBody, err := json.Marshal(struct {
		LeadID string `json:"lead_id"`
		Note   string `json:"note"`
		Title  string `json:"title,omitempty"`
		Pinned bool   `json:"pinned,omitempty"`
	}{input.LeadID, input.Note, input.Title, input.Pinned})
	if err != nil {
		return closeMutationBranch[Note]("createNote", CreateNoteBranchDefect, sdkgo.FailureLocalDefect, "note request could not be encoded")
	}
	return invokeCloseCreate(operation.client, call, "createNote", "/activity/note/", requestBody, CreateNoteBranchCreated, CreateNoteBranchProviderRejected, CreateNoteBranchDefect, decodeNote, validNoteID)
}

// UpdateNoteOperation replaces selected note fields.
type UpdateNoteOperation struct{ client *Client }

// UpdateNote binds this Mutation to the client.
func (client *Client) UpdateNote() UpdateNoteOperation { return UpdateNoteOperation{client} }

// Definition returns the generated updateNote contract.
func (UpdateNoteOperation) Definition() sdkgo.MutationDefinition { return UpdateNoteDefinition }

// IdempotencyKey correlates attempts; absolute field replacement is repeat safe.
func (UpdateNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces selected fields and classifies the Close response.
func (operation UpdateNoteOperation) Invoke(call sdkgo.Call, input UpdateNoteInput) sdkgo.MutationAttempt[Note] {
	if !validNoteID(input.NoteID) || (input.Note == nil && input.Title == nil && input.Pinned == nil) ||
		(input.Note != nil && (strings.TrimSpace(*input.Note) == "" || len(*input.Note) > 100000)) ||
		(input.Title != nil && len(*input.Title) > 1000) {
		return closeMutationBranch[Note]("updateNote", UpdateNoteBranchDefect, sdkgo.FailureValidation, "noteId and at least one valid field are required")
	}
	requestBody, err := json.Marshal(struct {
		Note   *string `json:"note,omitempty"`
		Title  *string `json:"title,omitempty"`
		Pinned *bool   `json:"pinned,omitempty"`
	}{input.Note, input.Title, input.Pinned})
	if err != nil {
		return closeMutationBranch[Note]("updateNote", UpdateNoteBranchDefect, sdkgo.FailureLocalDefect, "note update could not be encoded")
	}
	return invokeCloseWrite(operation.client, call, "updateNote", http.MethodPut, "/activity/note/"+input.NoteID+"/", requestBody, UpdateNoteBranchUpdated, UpdateNoteBranchNotFound, decodeNote)
}

// DeleteNoteOperation removes one note.
type DeleteNoteOperation struct{ client *Client }

// DeleteNote binds this Mutation to the client.
func (client *Client) DeleteNote() DeleteNoteOperation { return DeleteNoteOperation{client} }

// Definition returns the generated deleteNote contract.
func (DeleteNoteOperation) Definition() sdkgo.MutationDefinition { return DeleteNoteDefinition }

// IdempotencyKey correlates attempts; deleting an absent note is repeat safe.
func (DeleteNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes a note and classifies absence as completed deletion.
func (operation DeleteNoteOperation) Invoke(call sdkgo.Call, input DeleteNoteInput) sdkgo.MutationAttempt[DeletedNote] {
	if !validNoteID(input.NoteID) {
		return closeMutationBranch[DeletedNote]("deleteNote", DeleteNoteBranchDefect, sdkgo.FailureValidation, "noteId is invalid")
	}
	return invokeCloseWrite(operation.client, call, "deleteNote", http.MethodDelete, "/activity/note/"+input.NoteID+"/", nil, DeleteNoteBranchDeleted, DeleteNoteBranchDeleted, func(body []byte) (DeletedNote, error) {
		var response map[string]json.RawMessage
		if err := json.Unmarshal(body, &response); err != nil || response == nil {
			return DeletedNote{}, errors.New("invalid deletion response")
		}
		return DeletedNote{NoteID: input.NoteID}, nil
	})
}

func validNoteID(noteID string) bool {
	return strings.HasPrefix(noteID, "acti_") && safeKey.MatchString(noteID)
}

func decodeNote(body []byte) (Note, error) {
	var wire struct {
		ID     string `json:"id"`
		LeadID string `json:"lead_id"`
		Note   string `json:"note"`
		Title  string `json:"title"`
		Pinned bool   `json:"pinned"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return Note{}, err
	}
	if !validNoteID(wire.ID) || !validLeadID(wire.LeadID) {
		return Note{}, fmt.Errorf("incomplete note")
	}
	return Note{ID: wire.ID, LeadID: wire.LeadID, Note: wire.Note, Title: wire.Title, Pinned: wire.Pinned}, nil
}

func decodeNotePage(body []byte) (NotePage, error) {
	var wire struct {
		Data    []json.RawMessage `json:"data"`
		HasMore bool              `json:"has_more"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return NotePage{}, err
	}
	if wire.Data == nil {
		return NotePage{}, errors.New("note page is missing")
	}
	page := NotePage{Notes: make([]Note, 0, len(wire.Data)), HasMore: wire.HasMore}
	for _, row := range wire.Data {
		note, err := decodeNote(row)
		if err != nil {
			return NotePage{}, err
		}
		page.Notes = append(page.Notes, note)
	}
	return page, nil
}
