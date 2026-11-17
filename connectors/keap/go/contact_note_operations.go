// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package keap

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// ContactNoteIDInput selects a note attached to one contact.
type ContactNoteIDInput struct {
	// ContactID is the parent contact ID shown in Keap CRM.
	ContactID string `json:"contactId"`
	// NoteID is the note ID shown on that contact.
	NoteID string `json:"noteId"`
}

// ListContactNotesInput selects one page of a contact's notes.
type ListContactNotesInput struct {
	// ContactID is the parent contact ID.
	ContactID string `json:"contactId"`
	// PageSize is the maximum notes to return; zero uses 20.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is an opaque continuation token; blank starts the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// CreateContactNoteInput supplies a title, text, and Keap author identity.
type CreateContactNoteInput struct {
	// ContactID is the contact selected in Keap CRM > Contacts.
	ContactID string `json:"contactId"`
	// UserID is the author ID from Keap CRM > Settings > Users.
	UserID string `json:"userId"`
	// Title is the required note title.
	Title string `json:"title"`
	// Text is the note body; blank creates an empty body.
	Text string `json:"text"`
}

// UpdateContactNoteInput replaces a note's title and text.
type UpdateContactNoteInput struct {
	// ContactID is the parent contact ID.
	ContactID string `json:"contactId"`
	// NoteID is the note ID on that contact.
	NoteID string `json:"noteId"`
	// Title is the replacement title.
	Title string `json:"title"`
	// Text is the replacement body; blank clears it.
	Text string `json:"text"`
}

// ContactNote contains one note's identity, title, and text.
type ContactNote struct {
	// ID is the note's Keap ID.
	ID string `json:"id"`
	// ContactID is its parent contact ID.
	ContactID string `json:"contactId"`
	// Title is the note's title.
	Title string `json:"title"`
	// Text is the note's body.
	Text string `json:"text"`
}

// ContactNotesPage contains a page of notes and its continuation token.
type ContactNotesPage struct {
	// Notes are the returned notes.
	Notes []ContactNote `json:"notes"`
	// NextPageToken selects the next page, when present.
	NextPageToken string `json:"nextPageToken"`
}

// ContactNoteDeletion confirms deletion of one note.
type ContactNoteDeletion struct {
	// ContactID is the parent contact ID.
	ContactID string `json:"contactId"`
	// NoteID is the deleted note ID.
	NoteID string `json:"noteId"`
	// Deleted reports Keap's acceptance of the deletion.
	Deleted bool `json:"deleted"`
}

// GetContactNoteOperation reads one contact note.
type GetContactNoteOperation struct{ client *Client }

// ListContactNotesOperation reads one contact note page.
type ListContactNotesOperation struct{ client *Client }

// CreateContactNoteOperation creates a note with bounded sends.
type CreateContactNoteOperation struct{ client *Client }

// UpdateContactNoteOperation replaces a note's title and text.
type UpdateContactNoteOperation struct{ client *Client }

// DeleteContactNoteOperation deletes a note.
type DeleteContactNoteOperation struct{ client *Client }

// GetContactNote binds a note lookup to the client.
func (client *Client) GetContactNote() GetContactNoteOperation {
	return GetContactNoteOperation{client}
}

// ListContactNotes binds a note list to the client.
func (client *Client) ListContactNotes() ListContactNotesOperation {
	return ListContactNotesOperation{client}
}

// CreateContactNote binds note creation to the client.
func (client *Client) CreateContactNote() CreateContactNoteOperation {
	return CreateContactNoteOperation{client}
}

// UpdateContactNote binds note update to the client.
func (client *Client) UpdateContactNote() UpdateContactNoteOperation {
	return UpdateContactNoteOperation{client}
}

// DeleteContactNote binds note deletion to the client.
func (client *Client) DeleteContactNote() DeleteContactNoteOperation {
	return DeleteContactNoteOperation{client}
}

// Definition returns the generated query contract.
func (GetContactNoteOperation) Definition() sdkgo.QueryDefinition { return GetContactNoteDefinition }

// Definition returns the generated query contract.
func (ListContactNotesOperation) Definition() sdkgo.QueryDefinition {
	return ListContactNotesDefinition
}

// Definition returns the generated mutation contract.
func (CreateContactNoteOperation) Definition() sdkgo.MutationDefinition {
	return CreateContactNoteDefinition
}

// Definition returns the generated mutation contract.
func (UpdateContactNoteOperation) Definition() sdkgo.MutationDefinition {
	return UpdateContactNoteDefinition
}

// Definition returns the generated mutation contract.
func (DeleteContactNoteOperation) Definition() sdkgo.MutationDefinition {
	return DeleteContactNoteDefinition
}

// IdempotencyKey derives the stable Dex call identity.
func (CreateContactNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateContactNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives the stable Dex call identity.
func (UpdateContactNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateContactNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives the stable Dex call identity.
func (DeleteContactNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ ContactNoteIDInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one note from a contact.
func (operation GetContactNoteOperation) Invoke(call sdkgo.Call, input ContactNoteIDInput) sdkgo.QueryAttempt[ContactNote] {
	if !validContactID(input.ContactID) || !validContactID(input.NoteID) {
		return keapQueryBranch("getContactNote", GetContactNoteBranchDefect, ContactNote{}, sdkgo.FailureValidation, "contact and note IDs are required")
	}
	response := operation.client.request(call, http.MethodGet, contactNotePath(input.ContactID, input.NoteID), nil)
	if response.status == 404 {
		return keapQueryBranch("getContactNote", GetContactNoteBranchNotFound, ContactNote{}, sdkgo.FailureNotFound, "Keap did not find the note")
	}
	if branch, retry, handled := classifyKeapQuery(response, "getContactNote", GetContactNoteBranchProviderRejected, GetContactNoteBranchInvalidResponse, GetContactNoteBranchDefect, ContactNote{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	note, ok := decodeContactNote(response.body)
	if !ok {
		return keapQueryBranch("getContactNote", GetContactNoteBranchInvalidResponse, ContactNote{}, sdkgo.FailureProtocol, "Keap returned invalid note JSON")
	}
	return sdkgo.NewQueryBranch(GetContactNoteBranchFound, note, nil, sdkgo.Receipt{})
}

// Invoke lists one bounded page of a contact's notes.
func (operation ListContactNotesOperation) Invoke(call sdkgo.Call, input ListContactNotesInput) sdkgo.QueryAttempt[ContactNotesPage] {
	if !validContactID(input.ContactID) || input.PageSize < 0 || input.PageSize > 100 || len(input.PageToken) > 2048 {
		return keapQueryBranch("listContactNotes", ListContactNotesBranchDefect, ContactNotesPage{}, sdkgo.FailureValidation, "contact ID, page size, or token is invalid")
	}
	if input.PageSize == 0 {
		input.PageSize = 20
	}
	query := url.Values{"page_size": {strconv.Itoa(input.PageSize)}}
	if input.PageToken != "" {
		query.Set("page_token", input.PageToken)
	}
	response := operation.client.request(call, http.MethodGet, "/rest/v2/contacts/"+input.ContactID+"/notes?"+query.Encode(), nil)
	if response.status == 404 {
		return keapQueryBranch("listContactNotes", ListContactNotesBranchNotFound, ContactNotesPage{}, sdkgo.FailureNotFound, "Keap did not find the contact")
	}
	if branch, retry, handled := classifyKeapQuery(response, "listContactNotes", ListContactNotesBranchProviderRejected, ListContactNotesBranchInvalidResponse, ListContactNotesBranchDefect, ContactNotesPage{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	var wire struct {
		Notes         []json.RawMessage `json:"notes"`
		NextPageToken string            `json:"next_page_token"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Notes == nil {
		return keapQueryBranch("listContactNotes", ListContactNotesBranchInvalidResponse, ContactNotesPage{}, sdkgo.FailureProtocol, "Keap returned invalid note list JSON")
	}
	page := ContactNotesPage{Notes: make([]ContactNote, 0, len(wire.Notes)), NextPageToken: wire.NextPageToken}
	for _, raw := range wire.Notes {
		note, ok := decodeContactNote(raw)
		if !ok {
			return keapQueryBranch("listContactNotes", ListContactNotesBranchInvalidResponse, ContactNotesPage{}, sdkgo.FailureProtocol, "Keap returned invalid note identity")
		}
		page.Notes = append(page.Notes, note)
	}
	return sdkgo.NewQueryBranch(ListContactNotesBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke creates a note, retrying an unconfirmed send only within its cap.
func (operation CreateContactNoteOperation) Invoke(call sdkgo.Call, input CreateContactNoteInput) sdkgo.MutationAttempt[ContactNote] {
	if !validContactID(input.ContactID) || !validContactID(input.UserID) || !validNoteText(input.Title, 256) || len(input.Text) > 16384 {
		return keapMutationBranch("createContactNote", CreateContactNoteBranchDefect, ContactNote{}, sdkgo.FailureValidation, "contact, author, bounded title, and bounded text are required")
	}
	body, _ := json.Marshal(map[string]string{"user_id": input.UserID, "title": input.Title, "text": input.Text})
	fence, err := singledispatch.NewFence(call, CreateContactNoteDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return keapMutationBranch("createContactNote", CreateContactNoteBranchDefect, ContactNote{}, sdkgo.FailureLocalDefect, "note dispatch fence could not be constructed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(ContactNote{}, keapFailure("createContactNote", sdkgo.FailureTransport, "an earlier note create may have succeeded"), sdkgo.Receipt{})
	}
	canResend := decision.MaximumPriorSends+1 < call.MaxSends()
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.requestWithContext(requestContext, call, http.MethodPost, "/rest/v2/contacts/"+input.ContactID+"/notes", body)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[ContactNote](keapFailure("createContactNote", sdkgo.FailureAvailability, "note dispatch could not start"), 0)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(ContactNote{}, keapFailure("createContactNote", sdkgo.FailureAuthentication, "earlier note create may have succeeded"), sdkgo.Receipt{})
		}
		return keapMutationBranch("createContactNote", CreateContactNoteBranchDefect, ContactNote{}, sdkgo.FailureAuthentication, "Keap credentials are unavailable")
	}
	if response.status == 429 || response.status == 503 {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[ContactNote](keapFailure("createContactNote", sdkgo.FailureAvailability, "retry checkpoint failed"), 0)
		}
		return sdkgo.NewMutationRetry[ContactNote](keapFailure("createContactNote", sdkgo.FailureAvailability, "Keap did not process the note create"), response.retryAfter)
	}
	if errors.Is(response.err, errRequest) || response.status == 408 || response.status >= 500 || response.err != nil {
		failure := keapFailure("createContactNote", sdkgo.FailureTransport, "note create outcome is unknown")
		if canResend {
			return sdkgo.NewMutationRetry[ContactNote](failure, response.retryAfter)
		}
		return sdkgo.NewMutationUncertain(ContactNote{}, failure, sdkgo.Receipt{})
	}
	if response.status != 200 && response.status != 201 {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(ContactNote{}, keapFailure("createContactNote", sdkgo.FailureProviderRejection, "earlier note create may have succeeded"), sdkgo.Receipt{})
		}
		return keapMutationBranch("createContactNote", CreateContactNoteBranchProviderRejected, ContactNote{}, sdkgo.FailureProviderRejection, "Keap rejected note create")
	}
	note, ok := decodeContactNote(response.body)
	if !ok {
		return sdkgo.NewMutationUncertain(ContactNote{}, keapFailure("createContactNote", sdkgo.FailureProtocol, "created note identity cannot be confirmed"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(CreateContactNoteBranchCreated, note, nil, sdkgo.Receipt{})
}

// Invoke replaces a note's title and text.
func (operation UpdateContactNoteOperation) Invoke(call sdkgo.Call, input UpdateContactNoteInput) sdkgo.MutationAttempt[ContactNote] {
	if !validContactID(input.ContactID) || !validContactID(input.NoteID) || !validNoteText(input.Title, 256) || len(input.Text) > 16384 {
		return keapMutationBranch("updateContactNote", UpdateContactNoteBranchDefect, ContactNote{}, sdkgo.FailureValidation, "contact, note, bounded title, and bounded text are required")
	}
	body, _ := json.Marshal(map[string]string{"title": input.Title, "text": input.Text})
	response := operation.client.request(call, http.MethodPatch, contactNotePath(input.ContactID, input.NoteID)+"?update_mask=title%2Ctext", body)
	if response.status == 404 {
		return keapMutationBranch("updateContactNote", UpdateContactNoteBranchNotFound, ContactNote{}, sdkgo.FailureNotFound, "Keap did not find the note")
	}
	if branch, retry, handled := classifyKeapMutation(response, "updateContactNote", UpdateContactNoteBranchProviderRejected, UpdateContactNoteBranchInvalidResponse, UpdateContactNoteBranchDefect, ContactNote{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	if response.status != 200 {
		return keapMutationBranch("updateContactNote", UpdateContactNoteBranchProviderRejected, ContactNote{}, sdkgo.FailureProviderRejection, "Keap rejected note update")
	}
	note, ok := decodeContactNote(response.body)
	if !ok {
		return keapMutationBranch("updateContactNote", UpdateContactNoteBranchInvalidResponse, ContactNote{}, sdkgo.FailureProtocol, "Keap returned invalid note JSON")
	}
	return sdkgo.NewMutationBranch(UpdateContactNoteBranchUpdated, note, nil, sdkgo.Receipt{})
}

// Invoke deletes a note from a contact.
func (operation DeleteContactNoteOperation) Invoke(call sdkgo.Call, input ContactNoteIDInput) sdkgo.MutationAttempt[ContactNoteDeletion] {
	if !validContactID(input.ContactID) || !validContactID(input.NoteID) {
		return keapMutationBranch("deleteContactNote", DeleteContactNoteBranchDefect, ContactNoteDeletion{}, sdkgo.FailureValidation, "contact and note IDs are required")
	}
	response := operation.client.request(call, http.MethodDelete, contactNotePath(input.ContactID, input.NoteID), nil)
	if response.status == 404 {
		return keapMutationBranch("deleteContactNote", DeleteContactNoteBranchNotFound, ContactNoteDeletion{}, sdkgo.FailureNotFound, "Keap did not find the note")
	}
	if branch, retry, handled := classifyKeapMutation(response, "deleteContactNote", DeleteContactNoteBranchProviderRejected, DeleteContactNoteBranchInvalidResponse, DeleteContactNoteBranchDefect, ContactNoteDeletion{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	if response.status != 200 && response.status != 204 {
		return keapMutationBranch("deleteContactNote", DeleteContactNoteBranchProviderRejected, ContactNoteDeletion{}, sdkgo.FailureProviderRejection, "Keap rejected note deletion")
	}
	if response.status == 200 && len(response.body) > 0 && !json.Valid(response.body) {
		return keapMutationBranch("deleteContactNote", DeleteContactNoteBranchInvalidResponse, ContactNoteDeletion{}, sdkgo.FailureProtocol, "Keap returned invalid deletion JSON")
	}
	return sdkgo.NewMutationBranch(DeleteContactNoteBranchDeleted, ContactNoteDeletion{ContactID: input.ContactID, NoteID: input.NoteID, Deleted: true}, nil, sdkgo.Receipt{})
}

func contactNotePath(contactID, noteID string) string {
	return "/rest/v2/contacts/" + contactID + "/notes/" + noteID
}

func decodeContactNote(body []byte) (ContactNote, bool) {
	var wire struct {
		ID        string `json:"id"`
		ContactID string `json:"contact_id"`
		Title     string `json:"title"`
		Text      string `json:"text"`
	}
	if json.Unmarshal(body, &wire) != nil || !validContactID(wire.ID) || !validContactID(wire.ContactID) {
		return ContactNote{}, false
	}
	return ContactNote{ID: wire.ID, ContactID: wire.ContactID, Title: wire.Title, Text: wire.Text}, true
}

func validNoteText(value string, maximumBytes int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= maximumBytes
}
