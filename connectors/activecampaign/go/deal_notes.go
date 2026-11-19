// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListDealNotesInput selects one page of notes attached to a deal.
type ListDealNotesInput struct {
	// DealID is the numeric provider deal ID.
	DealID string `json:"dealId"`
	// Offset is the zero-based number of notes to skip.
	Offset int `json:"offset,omitempty"`
	// Limit is the maximum note count; zero uses 20 and 100 is the maximum.
	Limit int `json:"limit,omitempty"`
}

// GetDealNoteInput selects one deal note by its numeric note ID.
type GetDealNoteInput struct {
	// NoteID is the provider note ID.
	NoteID string `json:"noteId"`
}

// DealNoteMetadata contains one deal note without author or relationship links.
type DealNoteMetadata struct {
	// ID is the stable provider note ID.
	ID string `json:"id"`
	// DealID is the deal owning the note.
	DealID string `json:"dealId"`
	// Text is the note body.
	Text string `json:"text"`
}

// DealNotesPage is one offset-based page of deal notes.
type DealNotesPage struct {
	// Notes contains the selected notes.
	Notes []DealNoteMetadata `json:"notes"`
	// Offset is the requested zero-based offset.
	Offset int `json:"offset"`
	// Limit is the requested maximum count.
	Limit int `json:"limit"`
	// Total is the provider's matching note count.
	Total int `json:"total"`
}

// ListDealNotesOperation retrieves one page of notes attached to a deal.
type ListDealNotesOperation struct{ client *Client }

// GetDealNoteOperation retrieves one deal note by ID.
type GetDealNoteOperation struct{ client *Client }

// ListDealNotes binds the list Query to this client.
func (client *Client) ListDealNotes() ListDealNotesOperation { return ListDealNotesOperation{client} }

// GetDealNote binds the single-note Query to this client.
func (client *Client) GetDealNote() GetDealNoteOperation { return GetDealNoteOperation{client} }

// Definition returns the manifest-generated list contract.
func (ListDealNotesOperation) Definition() sdkgo.QueryDefinition { return ListDealNotesDefinition }

// Definition returns the manifest-generated single-note contract.
func (GetDealNoteOperation) Definition() sdkgo.QueryDefinition { return GetDealNoteDefinition }

// Invoke reads a bounded page of one deal's notes.
func (operation ListDealNotesOperation) Invoke(call sdkgo.Call, input ListDealNotesInput) sdkgo.QueryAttempt[DealNotesPage] {
	branches := queryBranches{found: ListDealNotesBranchListed, rejected: ListDealNotesBranchProviderRejected, invalid: ListDealNotesBranchInvalidResponse, defect: ListDealNotesBranchDefect}
	if !numericID.MatchString(input.DealID) || input.Offset < 0 || input.Limit < 0 || input.Limit > 100 {
		return queryBranch("listDealNotes", branches.defect, DealNotesPage{}, sdkgo.FailureValidation, "deal ID, offset, or limit is invalid")
	}
	if input.Limit == 0 {
		input.Limit = 20
	}
	query := url.Values{"filters[reltype]": {"Deal"}, "filters[relid]": {input.DealID}, "limit": {strconv.Itoa(input.Limit)}, "offset": {strconv.Itoa(input.Offset)}}
	return invokeQuery(call, operation.client, "listDealNotes", "/notes?"+query.Encode(), branches, func(body []byte) (DealNotesPage, error) { return decodeDealNotesPage(body, input) })
}

// Invoke reads one note and ensures it belongs to a deal.
func (operation GetDealNoteOperation) Invoke(call sdkgo.Call, input GetDealNoteInput) sdkgo.QueryAttempt[DealNoteMetadata] {
	branches := queryBranches{found: GetDealNoteBranchFound, notFound: GetDealNoteBranchNotFound, rejected: GetDealNoteBranchProviderRejected, invalid: GetDealNoteBranchInvalidResponse, defect: GetDealNoteBranchDefect}
	if !numericID.MatchString(input.NoteID) {
		return queryBranch("getDealNote", branches.defect, DealNoteMetadata{}, sdkgo.FailureValidation, "note ID must be numeric")
	}
	return invokeQuery(call, operation.client, "getDealNote", "/notes/"+input.NoteID, branches, decodeDealNote)
}

type dealNoteWire struct {
	ID           string `json:"id"`
	DealID       string `json:"relid"`
	RelationType string `json:"reltype"`
	Text         string `json:"note"`
}

func decodeDealNotesPage(body []byte, input ListDealNotesInput) (DealNotesPage, error) {
	var wire struct {
		Notes []dealNoteWire `json:"notes"`
		Meta  struct {
			Total string `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return DealNotesPage{}, err
	}
	if wire.Notes == nil {
		return DealNotesPage{}, fmt.Errorf("note collection missing")
	}
	total, err := strconv.Atoi(wire.Meta.Total)
	if err != nil || total < 0 {
		return DealNotesPage{}, fmt.Errorf("note count missing or invalid")
	}
	page := DealNotesPage{Notes: make([]DealNoteMetadata, 0, len(wire.Notes)), Offset: input.Offset, Limit: input.Limit, Total: total}
	for _, item := range wire.Notes {
		note, err := dealNoteMetadata(item)
		if err != nil || note.DealID != input.DealID {
			return DealNotesPage{}, fmt.Errorf("note does not belong to the selected deal")
		}
		page.Notes = append(page.Notes, note)
	}
	return page, nil
}

func decodeDealNote(body []byte) (DealNoteMetadata, error) {
	var wire struct {
		Note *dealNoteWire `json:"note"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return DealNoteMetadata{}, err
	}
	if wire.Note == nil {
		return DealNoteMetadata{}, fmt.Errorf("note missing")
	}
	return dealNoteMetadata(*wire.Note)
}

func dealNoteMetadata(item dealNoteWire) (DealNoteMetadata, error) {
	if !numericID.MatchString(item.ID) || !numericID.MatchString(item.DealID) || item.RelationType != "Deal" || item.Text == "" {
		return DealNoteMetadata{}, fmt.Errorf("deal note identity or text missing")
	}
	return DealNoteMetadata{ID: item.ID, DealID: item.DealID, Text: item.Text}, nil
}
