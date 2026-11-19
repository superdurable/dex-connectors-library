// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ashby

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListCandidateNotesInput selects one page of a candidate's notes.
type ListCandidateNotesInput struct {
	// CandidateID is the non-secret UUID of the candidate whose notes are listed.
	CandidateID string `json:"candidateId"`
	// Cursor is Ashby's opaque pagination cursor; blank starts at the first page.
	Cursor string `json:"cursor,omitempty"`
	// Limit caps the page at 100 notes; zero uses Ashby's default.
	Limit int `json:"limit,omitempty"`
}

// CandidateNoteSummary identifies a note without its content or author.
type CandidateNoteSummary struct {
	// ID is the note's UUID.
	ID string `json:"id"`
	// CreatedAt is Ashby's creation timestamp in UTC.
	CreatedAt string `json:"createdAt"`
	// IsPrivate reports whether Ashby marks the note private.
	IsPrivate bool `json:"isPrivate"`
}

// CandidateNotePage is a bounded page with the next cursor when present.
type CandidateNotePage struct {
	// Notes are the candidate's associated note identities.
	Notes []CandidateNoteSummary `json:"notes"`
	// NextCursor is passed to the next call when MoreDataAvailable is true.
	NextCursor string `json:"nextCursor,omitempty"`
	// MoreDataAvailable reports whether another page exists.
	MoreDataAvailable bool `json:"moreDataAvailable"`
}

// ListCandidateNotesOperation reads a candidate's note identities.
type ListCandidateNotesOperation struct{ client *Client }

// ListCandidateNotes binds the Query to this client.
func (client *Client) ListCandidateNotes() ListCandidateNotesOperation {
	return ListCandidateNotesOperation{client}
}

// Definition returns the manifest-generated Query definition.
func (ListCandidateNotesOperation) Definition() sdkgo.QueryDefinition {
	return ListCandidateNotesDefinition
}

// Invoke reads one page and omits note content and author details from the result.
func (operation ListCandidateNotesOperation) Invoke(call sdkgo.Call, input ListCandidateNotesInput) sdkgo.QueryAttempt[CandidateNotePage] {
	if !validCandidateID(input.CandidateID) || input.Limit < 0 || input.Limit > 100 || len(input.Cursor) > 1024 {
		return candidateNotesBranch(ListCandidateNotesBranchDefect, sdkgo.FailureValidation, "candidate note page input is invalid")
	}
	request := input
	if request.Limit == 0 {
		request.Limit = 100
	}
	response := operation.client.postOperation(call, "/candidate.listNotes", request)
	if errors.Is(response.err, errCredential) {
		return candidateNotesBranch(ListCandidateNotesBranchDefect, sdkgo.FailureAuthentication, "Ashby credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[CandidateNotePage](candidateNotesFailure(sdkgo.FailureTransport, "Ashby candidate notes request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[CandidateNotePage](candidateNotesFailure(sdkgo.FailureRateLimit, "Ashby rate limited candidate notes"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[CandidateNotePage](candidateNotesFailure(sdkgo.FailureAvailability, "Ashby candidate notes are unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return candidateNotesBranch(ListCandidateNotesBranchProviderRejected, sdkgo.FailureProviderRejection, "Ashby rejected candidate notes with HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return candidateNotesBranch(ListCandidateNotesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Ashby candidate note page exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[CandidateNotePage](candidateNotesFailure(sdkgo.FailureTransport, "Ashby candidate note response was interrupted"), 0)
	}
	var wire struct {
		Success           *bool                  `json:"success"`
		Results           []CandidateNoteSummary `json:"results"`
		NextCursor        string                 `json:"nextCursor"`
		MoreDataAvailable bool                   `json:"moreDataAvailable"`
		ErrorInfo         struct {
			Code string `json:"code"`
		} `json:"errorInfo"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Success == nil {
		return candidateNotesBranch(ListCandidateNotesBranchInvalidResponse, sdkgo.FailureProtocol, "Ashby returned invalid candidate note page JSON")
	}
	if !*wire.Success {
		if strings.TrimSpace(wire.ErrorInfo.Code) == "" {
			return candidateNotesBranch(ListCandidateNotesBranchInvalidResponse, sdkgo.FailureProtocol, "Ashby note rejection omitted its code")
		}
		return candidateNotesBranch(ListCandidateNotesBranchProviderRejected, sdkgo.FailureProviderRejection, "Ashby rejected candidate notes")
	}
	if wire.Results == nil || wire.MoreDataAvailable && wire.NextCursor == "" {
		return candidateNotesBranch(ListCandidateNotesBranchInvalidResponse, sdkgo.FailureProtocol, "Ashby candidate note page is incomplete")
	}
	for _, note := range wire.Results {
		if !validCandidateID(note.ID) || note.CreatedAt == "" {
			return candidateNotesBranch(ListCandidateNotesBranchInvalidResponse, sdkgo.FailureProtocol, "Ashby candidate note page contains an invalid note")
		}
	}
	return sdkgo.NewQueryBranch(ListCandidateNotesBranchListed, CandidateNotePage{Notes: wire.Results, NextCursor: wire.NextCursor, MoreDataAvailable: wire.MoreDataAvailable}, nil, sdkgo.Receipt{})
}

func candidateNotesFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "ashby", Operation: "listCandidateNotes", Message: message}
}

func candidateNotesBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[CandidateNotePage] {
	failure := candidateNotesFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, CandidateNotePage{}, &failure, sdkgo.Receipt{})
}
