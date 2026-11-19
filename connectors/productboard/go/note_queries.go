// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package productboard

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// NoteMetadata contains a note's identity without feedback content or owner details.
type NoteMetadata struct {
	// ID is the Productboard note UUID.
	ID string `json:"id"`
	// Type is textNote, conversationNote, or opportunityNote.
	Type string `json:"type"`
	// Name is the note title.
	Name string `json:"name"`
}

// NotesPage contains one page of note metadata and a cursor for the next page.
type NotesPage struct {
	// Notes excludes feedback content and people fields.
	Notes []NoteMetadata `json:"notes"`
	// NextCursor is blank on the last page.
	NextCursor string `json:"nextCursor"`
}

// ListNotesInput selects a page through an opaque Productboard cursor.
type ListNotesInput struct {
	// PageCursor is blank for the first page or a previous NextCursor.
	PageCursor string `json:"pageCursor,omitempty"`
}

// GetNoteInput identifies one note by UUID.
type GetNoteInput struct {
	// NoteID is the UUID from a Productboard note URL or API response.
	NoteID string `json:"noteId"`
}

// ListNotesOperation reads one page of note metadata.
type ListNotesOperation struct{ client *Client }

// ListNotes binds the Query to this client.
func (client *Client) ListNotes() ListNotesOperation { return ListNotesOperation{client} }

// Definition returns the manifest-generated Query contract.
func (ListNotesOperation) Definition() sdkgo.QueryDefinition { return ListNotesDefinition }

// Invoke reads one note page and excludes content from its Result.
func (operation ListNotesOperation) Invoke(call sdkgo.Call, input ListNotesInput) sdkgo.QueryAttempt[NotesPage] {
	if !validCursor(input.PageCursor) {
		return notesPageBranch(ListNotesBranchDefect, sdkgo.FailureValidation, "note page cursor is invalid")
	}
	query := url.Values{"fields[]": {"name"}}
	if input.PageCursor != "" {
		query.Set("pageCursor", input.PageCursor)
	}
	response := operation.client.get(call, "/notes?"+query.Encode())
	if errors.Is(response.err, errCredential) {
		return notesPageBranch(ListNotesBranchDefect, sdkgo.FailureAuthentication, "Productboard credentials are unavailable")
	}
	if kind, retry := featureRetryKind(response); retry {
		return sdkgo.NewQueryRetry[NotesPage](featureFailure("listNotes", kind, "Productboard notes are temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return notesPageBranch(ListNotesBranchProviderRejected, sdkgo.FailureProviderRejection, "Productboard rejected the note list")
	}
	if response.err != nil {
		return notesPageBranch(ListNotesBranchInvalidResponse, sdkgo.FailureProtocol, "Productboard note page could not be read")
	}
	page, valid := decodeNotesPage(response.body, operation.client.baseURL)
	if !valid {
		return notesPageBranch(ListNotesBranchInvalidResponse, sdkgo.FailureProtocol, "Productboard returned an invalid note page")
	}
	return sdkgo.NewQueryBranch(ListNotesBranchListed, page, nil, sdkgo.Receipt{})
}

// GetNoteOperation reads one note's ID, type, and name.
type GetNoteOperation struct{ client *Client }

// GetNote binds the Query to this client.
func (client *Client) GetNote() GetNoteOperation { return GetNoteOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetNoteOperation) Definition() sdkgo.QueryDefinition { return GetNoteDefinition }

// Invoke reads one note without returning its content.
func (operation GetNoteOperation) Invoke(call sdkgo.Call, input GetNoteInput) sdkgo.QueryAttempt[NoteMetadata] {
	if !validFeatureID(input.NoteID) {
		return noteQueryBranch(GetNoteBranchDefect, sdkgo.FailureValidation, "note ID must be a UUID")
	}
	response := operation.client.get(call, "/notes/"+url.PathEscape(input.NoteID)+"?fields=name")
	if errors.Is(response.err, errCredential) {
		return noteQueryBranch(GetNoteBranchDefect, sdkgo.FailureAuthentication, "Productboard credentials are unavailable")
	}
	if response.status == http.StatusNotFound {
		return noteQueryBranch(GetNoteBranchNotFound, sdkgo.FailureNotFound, "note was not found")
	}
	if kind, retry := featureRetryKind(response); retry {
		return sdkgo.NewQueryRetry[NoteMetadata](featureFailure("getNote", kind, "Productboard note lookup is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return noteQueryBranch(GetNoteBranchProviderRejected, sdkgo.FailureProviderRejection, "Productboard rejected the note lookup")
	}
	if response.err != nil {
		return noteQueryBranch(GetNoteBranchInvalidResponse, sdkgo.FailureProtocol, "Productboard note response could not be read")
	}
	note, valid := decodeNote(response.body)
	if !valid || note.ID != input.NoteID {
		return noteQueryBranch(GetNoteBranchInvalidResponse, sdkgo.FailureProtocol, "Productboard returned an invalid note")
	}
	return sdkgo.NewQueryBranch(GetNoteBranchFound, note, nil, sdkgo.Receipt{})
}

func decodeNotesPage(body []byte, baseURL string) (NotesPage, bool) {
	var wire struct {
		Data  []json.RawMessage `json:"data"`
		Links struct {
			Next string `json:"next"`
		} `json:"links"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Data == nil {
		return NotesPage{}, false
	}
	page := NotesPage{Notes: make([]NoteMetadata, 0, len(wire.Data))}
	for _, raw := range wire.Data {
		note, valid := decodeNoteData(raw)
		if !valid {
			return NotesPage{}, false
		}
		page.Notes = append(page.Notes, note)
	}
	if wire.Links.Next != "" {
		nextURL, err := url.Parse(wire.Links.Next)
		if err != nil || !validNotesNextURL(nextURL, baseURL) {
			return NotesPage{}, false
		}
		cursors := nextURL.Query()["pageCursor"]
		if len(cursors) != 1 || cursors[0] == "" || !validCursor(cursors[0]) {
			return NotesPage{}, false
		}
		page.NextCursor = cursors[0]
	}
	return page, true
}

func decodeNote(body []byte) (NoteMetadata, bool) {
	var wire struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil {
		return NoteMetadata{}, false
	}
	return decodeNoteData(wire.Data)
}

func decodeNoteData(body []byte) (NoteMetadata, bool) {
	var wire struct {
		ID     string `json:"id"`
		Type   string `json:"type"`
		Fields struct {
			Name string `json:"name"`
		} `json:"fields"`
	}
	if json.Unmarshal(body, &wire) != nil || !validFeatureID(wire.ID) || !validNoteType(wire.Type) || strings.TrimSpace(wire.Fields.Name) == "" {
		return NoteMetadata{}, false
	}
	return NoteMetadata{ID: wire.ID, Type: wire.Type, Name: wire.Fields.Name}, true
}

func validNoteType(noteType string) bool {
	return noteType == "textNote" || noteType == "conversationNote" || noteType == "opportunityNote"
}

func validNotesNextURL(nextURL *url.URL, baseURL string) bool {
	if nextURL.Scheme == "https" && nextURL.Host == "api.productboard.com" && nextURL.Path == "/v2/notes" {
		return true
	}
	return nextURL.Scheme+"://"+nextURL.Host+nextURL.Path == strings.TrimRight(baseURL, "/")+"/notes"
}

func notesPageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[NotesPage] {
	failure := featureFailure("listNotes", kind, message)
	return sdkgo.NewQueryBranch(branch, NotesPage{}, &failure, sdkgo.Receipt{})
}

func noteQueryBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[NoteMetadata] {
	failure := featureFailure("getNote", kind, message)
	return sdkgo.NewQueryBranch(branch, NoteMetadata{}, &failure, sdkgo.Receipt{})
}
