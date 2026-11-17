// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package nimble

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// ContactNote is a Nimble note attached to one or more contacts.
type ContactNote struct {
	// ID is the provider's note identifier.
	ID string `json:"id"`
	// Note is the complete note text.
	Note string `json:"note"`
	// NotePreview is the provider's short preview text.
	NotePreview string `json:"notePreview"`
}

// ContactNotesPage contains one page of notes and provider pagination metadata.
type ContactNotesPage struct {
	// Notes contains the returned notes, possibly empty.
	Notes []ContactNote `json:"notes"`
	// Page is the one-based page number.
	Page int `json:"page"`
	// Pages is the provider's total page count.
	Pages int `json:"pages"`
	// PerPage is the returned page size.
	PerPage int `json:"perPage"`
	// Total is the count of matching notes.
	Total int `json:"total"`
}

// ListContactNotesInput selects a note page for one contact.
type ListContactNotesInput struct {
	// ContactID is the 24-character Nimble contact ID.
	ContactID string `json:"contactId"`
	// Page is the one-based page number.
	Page int `json:"page"`
	// PerPage is the page size from 1 through 100.
	PerPage int `json:"perPage"`
}

// ListContactNotesOperation lists notes attached to a contact.
type ListContactNotesOperation struct{ client *Client }

// ListContactNotes binds the list Query to this client.
func (client *Client) ListContactNotes() ListContactNotesOperation {
	return ListContactNotesOperation{client}
}

// Definition returns the generated list Query contract.
func (ListContactNotesOperation) Definition() sdkgo.QueryDefinition {
	return ListContactNotesDefinition
}

// Invoke reads one note page from Nimble.
func (operation ListContactNotesOperation) Invoke(call sdkgo.Call, input ListContactNotesInput) sdkgo.QueryAttempt[ContactNotesPage] {
	branches := queryBranches{found: ListContactNotesBranchListed, notFound: ListContactNotesBranchNotFound, rejected: ListContactNotesBranchProviderRejected, invalid: ListContactNotesBranchInvalidResponse, defect: ListContactNotesBranchDefect}
	if !isContactID(input.ContactID) || input.Page < 1 || input.PerPage < 1 || input.PerPage > 100 {
		return queryBranch("listContactNotes", branches.defect, ContactNotesPage{}, sdkgo.FailureValidation, "contactId and bounded page are required")
	}
	query := url.Values{"page": {strconv.Itoa(input.Page)}, "per_page": {strconv.Itoa(input.PerPage)}}
	return invokeQuery(call, operation.client, "listContactNotes", "/contacts/"+url.PathEscape(input.ContactID)+"/notes?"+query.Encode(), branches, decodeContactNotesPage)
}

func decodeContactNotesPage(body []byte) (ContactNotesPage, error) {
	var wire struct {
		Meta struct {
			Page    int `json:"page"`
			Pages   int `json:"pages"`
			PerPage int `json:"per_page"`
			Total   int `json:"total"`
		} `json:"meta"`
		Resources []struct {
			ID          string `json:"id"`
			Note        string `json:"note"`
			NotePreview string `json:"note_preview"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactNotesPage{}, err
	}
	if wire.Resources == nil || wire.Meta.Page < 1 || wire.Meta.PerPage < 1 || wire.Meta.Pages < 0 || wire.Meta.Total < 0 {
		return ContactNotesPage{}, fmt.Errorf("note page is incomplete")
	}
	notes := make([]ContactNote, 0, len(wire.Resources))
	for _, note := range wire.Resources {
		if !isContactID(note.ID) {
			return ContactNotesPage{}, fmt.Errorf("note ID is invalid")
		}
		notes = append(notes, ContactNote{ID: note.ID, Note: note.Note, NotePreview: note.NotePreview})
	}
	return ContactNotesPage{Notes: notes, Page: wire.Meta.Page, Pages: wire.Meta.Pages, PerPage: wire.Meta.PerPage, Total: wire.Meta.Total}, nil
}

// GetContactNoteInput identifies one provider note.
type GetContactNoteInput struct {
	// NoteID is the 24-character Nimble note ID.
	NoteID string `json:"noteId"`
}

// GetContactNoteOperation reads one contact note.
type GetContactNoteOperation struct{ client *Client }

// GetContactNote binds the read Query to this client.
func (client *Client) GetContactNote() GetContactNoteOperation {
	return GetContactNoteOperation{client}
}

// Definition returns the generated read Query contract.
func (GetContactNoteOperation) Definition() sdkgo.QueryDefinition { return GetContactNoteDefinition }

// Invoke reads one note by ID.
func (operation GetContactNoteOperation) Invoke(call sdkgo.Call, input GetContactNoteInput) sdkgo.QueryAttempt[ContactNote] {
	branches := queryBranches{found: GetContactNoteBranchFound, notFound: GetContactNoteBranchNotFound, rejected: GetContactNoteBranchProviderRejected, invalid: GetContactNoteBranchInvalidResponse, defect: GetContactNoteBranchDefect}
	if !isContactID(input.NoteID) {
		return queryBranch("getContactNote", branches.defect, ContactNote{}, sdkgo.FailureValidation, "noteId is invalid")
	}
	return invokeQuery(call, operation.client, "getContactNote", "/contacts/notes/"+url.PathEscape(input.NoteID), branches, decodeContactNote)
}

func decodeContactNote(body []byte) (ContactNote, error) {
	var wire struct {
		ID          string `json:"id"`
		Note        string `json:"note"`
		NotePreview string `json:"note_preview"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ContactNote{}, err
	}
	if !isContactID(wire.ID) {
		return ContactNote{}, fmt.Errorf("note ID is invalid")
	}
	return ContactNote{ID: wire.ID, Note: wire.Note, NotePreview: wire.NotePreview}, nil
}

// ContactNoteWrite supplies the required note text and contact associations.
type ContactNoteWrite struct {
	// ContactIDs lists one to ten 24-character contact IDs.
	ContactIDs []string `json:"contactIds"`
	// Note is the complete non-empty note text.
	Note string `json:"note"`
	// NotePreview is the non-empty short preview.
	NotePreview string `json:"notePreview"`
}

// CreateContactNoteInput supplies one new contact note.
type CreateContactNoteInput struct {
	// ContactIDs lists one to ten 24-character contact IDs.
	ContactIDs []string `json:"contactIds"`
	// Note is the complete non-empty note text.
	Note string `json:"note"`
	// NotePreview is the non-empty short preview.
	NotePreview string `json:"notePreview"`
}

// UpdateContactNoteInput identifies and replaces a note.
type UpdateContactNoteInput struct {
	// NoteID is the 24-character note ID.
	NoteID string `json:"noteId"`
	// ContactIDs lists one to ten 24-character contact IDs.
	ContactIDs []string `json:"contactIds"`
	// Note is the complete non-empty note text.
	Note string `json:"note"`
	// NotePreview is the non-empty short preview.
	NotePreview string `json:"notePreview"`
}

func validateContactNoteWrite(input ContactNoteWrite) error {
	if len(input.ContactIDs) < 1 || len(input.ContactIDs) > 10 || strings.TrimSpace(input.Note) == "" || strings.TrimSpace(input.NotePreview) == "" {
		return fmt.Errorf("one to ten contactIds, note, and notePreview are required")
	}
	for _, id := range input.ContactIDs {
		if !isContactID(id) {
			return fmt.Errorf("contactIds contains an invalid ID")
		}
	}
	return nil
}

func encodeContactNoteWrite(input ContactNoteWrite) ([]byte, error) {
	return json.Marshal(struct {
		ContactIDs  []string `json:"contact_ids"`
		Note        string   `json:"note"`
		NotePreview string   `json:"note_preview"`
	}{ContactIDs: input.ContactIDs, Note: input.Note, NotePreview: input.NotePreview})
}

// CreateContactNoteOperation creates a note under a limited send cap.
type CreateContactNoteOperation struct{ client *Client }

// CreateContactNote binds note creation to this client.
func (client *Client) CreateContactNote() CreateContactNoteOperation {
	return CreateContactNoteOperation{client}
}

// Definition returns the generated note creation contract.
func (CreateContactNoteOperation) Definition() sdkgo.MutationDefinition {
	return CreateContactNoteDefinition
}

// IdempotencyKey identifies this call across retries; Nimble accepts no key header.
func (CreateContactNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateContactNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a note under a persisted send cap.
func (operation CreateContactNoteOperation) Invoke(call sdkgo.Call, input CreateContactNoteInput) sdkgo.MutationAttempt[ContactNote] {
	noteWrite := ContactNoteWrite{ContactIDs: input.ContactIDs, Note: input.Note, NotePreview: input.NotePreview}
	if err := validateContactNoteWrite(noteWrite); err != nil {
		return mutationBranch(CreateContactNoteBranchDefect, "createContactNote", ContactNote{}, sdkgo.FailureValidation, err.Error())
	}
	body, err := encodeContactNoteWrite(noteWrite)
	if err != nil {
		return mutationBranch(CreateContactNoteBranchDefect, "createContactNote", ContactNote{}, sdkgo.FailureLocalDefect, "note body could not be built")
	}
	fence, err := singledispatch.NewFence(call, CreateContactNoteDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return mutationBranch(CreateContactNoteBranchDefect, "createContactNote", ContactNote{}, sdkgo.FailureLocalDefect, "dispatch checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(ContactNote{}, queryFailure("createContactNote", sdkgo.FailureTransport, "an earlier note create may have succeeded"), sdkgo.Receipt{})
	}
	canResend := decision.MaximumPriorSends+1 < call.MaxSends()
	var response writeResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		var sendError error
		response, sendError = operation.client.sendWrite(call, requestContext, http.MethodPost, "/contacts/notes", body)
		return sendError
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[ContactNote](queryFailure("createContactNote", sdkgo.FailureAvailability, "note was not dispatched"), 0)
	}
	if errors.Is(err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(ContactNote{}, queryFailure("createContactNote", sdkgo.FailureAuthentication, "earlier note create may have succeeded"), sdkgo.Receipt{})
		}
		if fence.RecordNotSent() != nil {
			return sdkgo.NewMutationRetry[ContactNote](queryFailure("createContactNote", sdkgo.FailureAvailability, "no-send checkpoint failed"), 0)
		}
		return mutationBranch(CreateContactNoteBranchDefect, "createContactNote", ContactNote{}, sdkgo.FailureAuthentication, "credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[ContactNote](queryFailure("createContactNote", sdkgo.FailureAvailability, "retry checkpoint failed"), 0)
		}
		failureKind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			failureKind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[ContactNote](queryFailure("createContactNote", failureKind, "Nimble temporarily rejected the note"), response.retryAfter)
	}
	if err != nil || response.status >= 500 {
		failure := queryFailure("createContactNote", sdkgo.FailureAvailability, "note create outcome is unknown")
		if canResend {
			return sdkgo.NewMutationRetry[ContactNote](failure, response.retryAfter)
		}
		return sdkgo.NewMutationUncertain(ContactNote{}, failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(ContactNote{}, queryFailure("createContactNote", sdkgo.FailureProviderRejection, "earlier note create may have succeeded"), sdkgo.Receipt{})
		}
		return mutationBranch(CreateContactNoteBranchProviderRejected, "createContactNote", ContactNote{}, sdkgo.FailureProviderRejection, "Nimble rejected the note")
	}
	note, parseError := decodeContactNote(response.body)
	if parseError != nil {
		return sdkgo.NewMutationUncertain(ContactNote{}, queryFailure("createContactNote", sdkgo.FailureProtocol, "Nimble accepted the note but did not identify it"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(CreateContactNoteBranchCreated, note, nil, sdkgo.Receipt{})
}

// UpdateContactNoteOperation replaces a contact note.
type UpdateContactNoteOperation struct{ client *Client }

// UpdateContactNote binds note replacement to this client.
func (client *Client) UpdateContactNote() UpdateContactNoteOperation {
	return UpdateContactNoteOperation{client}
}

// Definition returns the generated note replacement contract.
func (UpdateContactNoteOperation) Definition() sdkgo.MutationDefinition {
	return UpdateContactNoteDefinition
}

// IdempotencyKey identifies the absolute replacement across attempts.
func (UpdateContactNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateContactNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the note text and associations.
func (operation UpdateContactNoteOperation) Invoke(call sdkgo.Call, input UpdateContactNoteInput) sdkgo.MutationAttempt[ContactNote] {
	if !isContactID(input.NoteID) {
		return mutationBranch(UpdateContactNoteBranchDefect, "updateContactNote", ContactNote{}, sdkgo.FailureValidation, "noteId is invalid")
	}
	noteWrite := ContactNoteWrite{ContactIDs: input.ContactIDs, Note: input.Note, NotePreview: input.NotePreview}
	if err := validateContactNoteWrite(noteWrite); err != nil {
		return mutationBranch(UpdateContactNoteBranchDefect, "updateContactNote", ContactNote{}, sdkgo.FailureValidation, err.Error())
	}
	body, err := encodeContactNoteWrite(noteWrite)
	if err != nil {
		return mutationBranch(UpdateContactNoteBranchDefect, "updateContactNote", ContactNote{}, sdkgo.FailureLocalDefect, "note body could not be built")
	}
	response, err := operation.client.sendWrite(call, call.Context, http.MethodPut, "/contacts/notes/"+url.PathEscape(input.NoteID), body)
	if errors.Is(err, errCredential) {
		return mutationBranch(UpdateContactNoteBranchDefect, "updateContactNote", ContactNote{}, sdkgo.FailureAuthentication, "credentials are unavailable")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[ContactNote](queryFailure("updateContactNote", sdkgo.FailureTransport, "note replacement failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[ContactNote](queryFailure("updateContactNote", sdkgo.FailureAvailability, "note replacement is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return mutationBranch(UpdateContactNoteBranchNotFound, "updateContactNote", ContactNote{}, sdkgo.FailureNotFound, "note was not found")
	}
	if response.status != http.StatusOK {
		return mutationBranch(UpdateContactNoteBranchProviderRejected, "updateContactNote", ContactNote{}, sdkgo.FailureProviderRejection, "Nimble rejected the note replacement")
	}
	note, parseError := decodeContactNote(response.body)
	if parseError != nil {
		return mutationBranch(UpdateContactNoteBranchInvalidResponse, "updateContactNote", ContactNote{}, sdkgo.FailureProtocol, "Nimble returned an invalid note")
	}
	return sdkgo.NewMutationBranch(UpdateContactNoteBranchUpdated, note, nil, sdkgo.Receipt{})
}

// DeleteContactNoteInput identifies one note for deletion.
type DeleteContactNoteInput struct {
	// NoteID is the 24-character Nimble note ID.
	NoteID string `json:"noteId"`
}

// DeletedContactNote identifies the removed note.
type DeletedContactNote struct {
	// NoteID is the 24-character Nimble note ID.
	NoteID string `json:"noteId"`
}

// DeleteContactNoteOperation deletes one note.
type DeleteContactNoteOperation struct{ client *Client }

// DeleteContactNote binds note deletion to this client.
func (client *Client) DeleteContactNote() DeleteContactNoteOperation {
	return DeleteContactNoteOperation{client}
}

// Definition returns the generated note deletion contract.
func (DeleteContactNoteOperation) Definition() sdkgo.MutationDefinition {
	return DeleteContactNoteDefinition
}

// IdempotencyKey identifies the repeated deletion.
func (DeleteContactNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteContactNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes one note and returns its ID.
func (operation DeleteContactNoteOperation) Invoke(call sdkgo.Call, input DeleteContactNoteInput) sdkgo.MutationAttempt[DeletedContactNote] {
	if !isContactID(input.NoteID) {
		return mutationBranch(DeleteContactNoteBranchDefect, "deleteContactNote", DeletedContactNote{}, sdkgo.FailureValidation, "noteId is invalid")
	}
	response, err := operation.client.sendWrite(call, call.Context, http.MethodDelete, "/contacts/notes/"+url.PathEscape(input.NoteID), nil)
	if errors.Is(err, errCredential) {
		return mutationBranch(DeleteContactNoteBranchDefect, "deleteContactNote", DeletedContactNote{}, sdkgo.FailureAuthentication, "credentials are unavailable")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[DeletedContactNote](queryFailure("deleteContactNote", sdkgo.FailureTransport, "note deletion failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[DeletedContactNote](queryFailure("deleteContactNote", sdkgo.FailureAvailability, "note deletion is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return mutationBranch(DeleteContactNoteBranchNotFound, "deleteContactNote", DeletedContactNote{}, sdkgo.FailureNotFound, "note was not found")
	}
	if response.status != http.StatusOK {
		return mutationBranch(DeleteContactNoteBranchProviderRejected, "deleteContactNote", DeletedContactNote{}, sdkgo.FailureProviderRejection, "Nimble rejected the note deletion")
	}
	return sdkgo.NewMutationBranch(DeleteContactNoteBranchDeleted, DeletedContactNote{NoteID: input.NoteID}, nil, sdkgo.Receipt{})
}
