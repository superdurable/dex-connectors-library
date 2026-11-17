// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lever

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// NoteSummary contains note metadata without candidate note content.
type NoteSummary struct {
	// ID is the Lever note UID.
	ID           string `json:"id"`
	// CreatedAt is Lever's creation time in Unix milliseconds.
	CreatedAt    int64  `json:"createdAt"`
	// CommentCount counts comments without exposing their text.
	CommentCount int    `json:"commentCount"`
}

// NotesPage contains note metadata from one opportunity.
type NotesPage struct {
	// Notes contains metadata for each visible note.
	Notes []NoteSummary `json:"notes"`
}

// NoteIdentity identifies a note confirmed by Lever.
type NoteIdentity struct {
	// ID is the Lever note UID.
	ID string `json:"id"`
}

// GetNoteInput identifies a note on one opportunity.
type GetNoteInput struct {
	// OpportunityID is the candidate opportunity UID.
	OpportunityID string `json:"opportunityId"`
	// NoteID is the note UID.
	NoteID        string `json:"noteId"`
}

// ListNotesInput identifies an opportunity whose notes should be listed.
type ListNotesInput struct {
	// OpportunityID is the candidate opportunity UID.
	OpportunityID string `json:"opportunityId"`
}

// CreateNoteInput supplies text for a new note on one opportunity.
type CreateNoteInput struct {
	// OpportunityID is the candidate opportunity UID.
	OpportunityID string `json:"opportunityId"`
	// Value is the required note text.
	Value         string `json:"value"`
}

// UpdateNoteInput replaces all comments of one API-created note.
type UpdateNoteInput struct {
	// OpportunityID is the candidate opportunity UID.
	OpportunityID string   `json:"opportunityId"`
	// NoteID is the UID of a note created through the API.
	NoteID        string   `json:"noteId"`
	// Values contains every replacement comment, in order.
	Values        []string `json:"values"`
}

// DeleteNoteInput identifies an API-created note to delete.
type DeleteNoteInput struct {
	// OpportunityID is the candidate opportunity UID.
	OpportunityID string `json:"opportunityId"`
	// NoteID is the UID of a note created through the API.
	NoteID        string `json:"noteId"`
}

// GetNoteOperation reads note metadata.
type GetNoteOperation struct{ client *Client }

// GetNote binds the Query to this client.
func (client *Client) GetNote() GetNoteOperation { return GetNoteOperation{client} }

// Definition returns the generated Query contract.
func (GetNoteOperation) Definition() sdkgo.QueryDefinition { return GetNoteDefinition }

// Invoke reads one note without returning its text.
func (operation GetNoteOperation) Invoke(call sdkgo.Call, input GetNoteInput) sdkgo.QueryAttempt[NoteSummary] {
	if !validOpportunityID(input.OpportunityID) || !validOpportunityID(input.NoteID) {
		return postingQueryBranch[NoteSummary]("getNote", GetNoteBranchDefect, sdkgo.FailureValidation, "note or opportunity ID is invalid")
	}
	return readPostingQuery(operation.client.get(call, notePath(input.OpportunityID, input.NoteID)), "getNote", GetNoteBranchFound, GetNoteBranchNotFound, GetNoteBranchProviderRejected, GetNoteBranchInvalidResponse, GetNoteBranchDefect, decodeNote)
}

// ListNotesOperation lists one opportunity's note metadata.
type ListNotesOperation struct{ client *Client }

// ListNotes binds the Query to this client.
func (client *Client) ListNotes() ListNotesOperation { return ListNotesOperation{client} }

// Definition returns the generated Query contract.
func (ListNotesOperation) Definition() sdkgo.QueryDefinition { return ListNotesDefinition }

// Invoke lists notes without returning their text.
func (operation ListNotesOperation) Invoke(call sdkgo.Call, input ListNotesInput) sdkgo.QueryAttempt[NotesPage] {
	if !validOpportunityID(input.OpportunityID) {
		return postingQueryBranch[NotesPage]("listNotes", ListNotesBranchDefect, sdkgo.FailureValidation, "opportunity ID is invalid")
	}
	return readPostingQuery(operation.client.get(call, notePath(input.OpportunityID, "")), "listNotes", ListNotesBranchListed, "", ListNotesBranchProviderRejected, ListNotesBranchInvalidResponse, ListNotesBranchDefect, decodeNotes)
}

// CreateNoteOperation creates one note.
type CreateNoteOperation struct{ client *Client }

// CreateNote binds the Mutation to this client.
func (client *Client) CreateNote() CreateNoteOperation { return CreateNoteOperation{client} }

// Definition returns the generated Mutation contract.
func (CreateNoteOperation) Definition() sdkgo.MutationDefinition { return CreateNoteDefinition }

// IdempotencyKey correlates repeats; Lever does not deduplicate this create by key.
func (CreateNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one note with bounded sends and a durable dispatch fence.
func (operation CreateNoteOperation) Invoke(call sdkgo.Call, input CreateNoteInput) sdkgo.MutationAttempt[NoteIdentity] {
	if !validOpportunityID(input.OpportunityID) || strings.TrimSpace(input.Value) == "" {
		return noteMutationBranch("createNote", CreateNoteBranchDefect, sdkgo.FailureValidation, "opportunity ID or note text is invalid", sdkgo.Receipt{})
	}
	receipt := sdkgo.Receipt{CallID: call.ID, Provider: "lever"}
	fence, err := singledispatch.NewFence(call, CreateNoteDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return noteMutationBranch("createNote", CreateNoteBranchDefect, sdkgo.FailureLocalDefect, "note dispatch checkpoint failed", receipt)
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(NoteIdentity{}, noteFailure("createNote", sdkgo.FailureTransport, "note may have been created at the send cap"), receipt)
	}
	if decision.Marker.ProviderID != "" {
		readback := operation.client.get(call, notePath(input.OpportunityID, decision.Marker.ProviderID))
		if readback.status == http.StatusOK && readback.err == nil {
			summary, err := decodeNote(readback.body)
			if err == nil && summary.ID == decision.Marker.ProviderID {
				receipt.ProviderObjectID = summary.ID
				return sdkgo.NewMutationBranch(CreateNoteBranchCreated, NoteIdentity{ID: summary.ID}, nil, receipt)
			}
		}
		return sdkgo.NewMutationRetry[NoteIdentity](noteFailure("createNote", sdkgo.FailureAvailability, "created note read-back unavailable"), readback.retryAfter)
	}
	payload, err := json.Marshal(struct {
		Value string `json:"value"`
	}{Value: input.Value})
	if err != nil {
		return noteMutationBranch("createNote", CreateNoteBranchDefect, sdkgo.FailureLocalDefect, "note request encoding failed", receipt)
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.doWithContext(call, requestContext, http.MethodPost, notePath(input.OpportunityID, ""), payload)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[NoteIdentity](noteFailure("createNote", sdkgo.FailureAvailability, "note send claim was not durable"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[NoteIdentity](noteFailure("createNote", sdkgo.FailureAvailability, "note retry checkpoint failed"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[NoteIdentity](noteFailure("createNote", kind, "Lever temporarily refused note creation"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(NoteIdentity{}, noteFailure("createNote", sdkgo.FailureAuthentication, "credentials unavailable after a possible create"), receipt)
		}
		return noteMutationBranch("createNote", CreateNoteBranchDefect, sdkgo.FailureAuthentication, "Lever credentials unavailable", receipt)
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return unknownNoteCreate(call, decision.MaximumPriorSends, receipt, sdkgo.FailureTransport, "note create outcome is unknown")
	}
	if response.status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(NoteIdentity{}, noteFailure("createNote", sdkgo.FailureProviderRejection, "Lever rejected a resend after a possible create"), receipt)
		}
		return noteMutationBranch("createNote", CreateNoteBranchProviderRejected, sdkgo.FailureProviderRejection, "Lever rejected note creation", receipt)
	}
	identity, err := decodeNoteIdentity(response.body)
	if err != nil {
		return unknownNoteCreate(call, decision.MaximumPriorSends, receipt, sdkgo.FailureProtocol, "Lever did not confirm note ID")
	}
	if err := fence.RecordProviderID(identity.ID); err != nil {
		return sdkgo.NewMutationRetry[NoteIdentity](noteFailure("createNote", sdkgo.FailureAvailability, "note ID checkpoint failed"), 0)
	}
	receipt.ProviderObjectID = identity.ID
	return sdkgo.NewMutationBranch(CreateNoteBranchCreated, identity, nil, receipt)
}

// UpdateNoteOperation replaces an API-created note's comments.
type UpdateNoteOperation struct{ client *Client }

// UpdateNote binds the Mutation to this client.
func (client *Client) UpdateNote() UpdateNoteOperation { return UpdateNoteOperation{client} }

// Definition returns the generated Mutation contract.
func (UpdateNoteOperation) Definition() sdkgo.MutationDefinition { return UpdateNoteDefinition }

// IdempotencyKey correlates absolute replacements.
func (UpdateNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces all comments and confirms Lever's note ID.
func (operation UpdateNoteOperation) Invoke(call sdkgo.Call, input UpdateNoteInput) sdkgo.MutationAttempt[NoteIdentity] {
	if !validOpportunityID(input.OpportunityID) || !validOpportunityID(input.NoteID) || len(input.Values) == 0 {
		return noteMutationBranch("updateNote", UpdateNoteBranchDefect, sdkgo.FailureValidation, "note ID or replacement comments are invalid", sdkgo.Receipt{})
	}
	values := make([]struct {
		Value string `json:"value"`
	}, 0, len(input.Values))
	for _, value := range input.Values {
		if strings.TrimSpace(value) == "" {
			return noteMutationBranch("updateNote", UpdateNoteBranchDefect, sdkgo.FailureValidation, "note comment is blank", sdkgo.Receipt{})
		}
		values = append(values, struct {
			Value string `json:"value"`
		}{Value: value})
	}
	receipt := sdkgo.Receipt{CallID: call.ID, Provider: "lever", ProviderObjectID: input.NoteID}
	payload, err := json.Marshal(struct {
		Values []struct {
			Value string `json:"value"`
		} `json:"values"`
	}{Values: values})
	if err != nil {
		return noteMutationBranch("updateNote", UpdateNoteBranchDefect, sdkgo.FailureLocalDefect, "note replacement encoding failed", receipt)
	}
	response := operation.client.do(call, http.MethodPut, notePath(input.OpportunityID, input.NoteID), payload)
	return classifyNoteWrite(response, "updateNote", UpdateNoteBranchUpdated, UpdateNoteBranchNotFound, UpdateNoteBranchProviderRejected, UpdateNoteBranchDefect, input.NoteID, receipt)
}

// DeleteNoteOperation deletes an API-created note.
type DeleteNoteOperation struct{ client *Client }

// DeleteNote binds the Mutation to this client.
func (client *Client) DeleteNote() DeleteNoteOperation { return DeleteNoteOperation{client} }

// Definition returns the generated Mutation contract.
func (DeleteNoteOperation) Definition() sdkgo.MutationDefinition { return DeleteNoteDefinition }

// IdempotencyKey correlates repeated deletes.
func (DeleteNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes one note; a missing note confirms the requested absent state.
func (operation DeleteNoteOperation) Invoke(call sdkgo.Call, input DeleteNoteInput) sdkgo.MutationAttempt[NoteIdentity] {
	if !validOpportunityID(input.OpportunityID) || !validOpportunityID(input.NoteID) {
		return noteMutationBranch("deleteNote", DeleteNoteBranchDefect, sdkgo.FailureValidation, "note or opportunity ID is invalid", sdkgo.Receipt{})
	}
	receipt := sdkgo.Receipt{CallID: call.ID, Provider: "lever", ProviderObjectID: input.NoteID}
	response := operation.client.do(call, http.MethodDelete, notePath(input.OpportunityID, input.NoteID), nil)
	return classifyNoteWrite(response, "deleteNote", DeleteNoteBranchDeleted, "", DeleteNoteBranchProviderRejected, DeleteNoteBranchDefect, input.NoteID, receipt)
}

func notePath(opportunityID, noteID string) string {
	path := "/opportunities/" + url.PathEscape(opportunityID) + "/notes"
	if noteID != "" {
		path += "/" + url.PathEscape(noteID)
	}
	return path
}

func decodeNote(body []byte) (NoteSummary, error) {
	var wire struct {
		Data struct {
			ID        string            `json:"id"`
			CreatedAt int64             `json:"createdAt"`
			Fields    []json.RawMessage `json:"fields"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || !validOpportunityID(wire.Data.ID) || wire.Data.CreatedAt <= 0 || wire.Data.Fields == nil {
		return NoteSummary{}, fmt.Errorf("Lever note JSON invalid")
	}
	return NoteSummary{ID: wire.Data.ID, CreatedAt: wire.Data.CreatedAt, CommentCount: len(wire.Data.Fields)}, nil
}

func decodeNotes(body []byte) (NotesPage, error) {
	var wire struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Data == nil {
		return NotesPage{}, fmt.Errorf("Lever notes JSON invalid")
	}
	page := NotesPage{Notes: make([]NoteSummary, 0, len(wire.Data))}
	for _, entry := range wire.Data {
		note, err := decodeNote(append([]byte(`{"data":`), append(entry, '}')...))
		if err != nil {
			return NotesPage{}, err
		}
		page.Notes = append(page.Notes, note)
	}
	return page, nil
}

func decodeNoteIdentity(body []byte) (NoteIdentity, error) {
	var wire struct {
		Data struct {
			NoteID string `json:"noteId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || !validOpportunityID(wire.Data.NoteID) {
		return NoteIdentity{}, fmt.Errorf("Lever note identity invalid")
	}
	return NoteIdentity{ID: wire.Data.NoteID}, nil
}

func unknownNoteCreate(call sdkgo.Call, priorSends int32, receipt sdkgo.Receipt, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[NoteIdentity] {
	if priorSends+1 < call.MaxSends() {
		return sdkgo.NewMutationRetry[NoteIdentity](noteFailure("createNote", kind, message), 0)
	}
	return sdkgo.NewMutationUncertain(NoteIdentity{}, noteFailure("createNote", kind, message+" at the send cap"), receipt)
}

func classifyNoteWrite(response providerResponse, operation string, success, notFound, rejected, defect sdkgo.BranchID, noteID string, receipt sdkgo.Receipt) sdkgo.MutationAttempt[NoteIdentity] {
	if errors.Is(response.err, errCredential) {
		return noteMutationBranch(operation, defect, sdkgo.FailureAuthentication, "Lever credentials unavailable", receipt)
	}
	if errors.Is(response.err, errRequest) || response.err != nil {
		return sdkgo.NewMutationRetry[NoteIdentity](noteFailure(operation, sdkgo.FailureTransport, "note write outcome is unknown"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[NoteIdentity](noteFailure(operation, sdkgo.FailureRateLimit, "Lever rate limited note write"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[NoteIdentity](noteFailure(operation, sdkgo.FailureAvailability, "note write outcome is unknown"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		if notFound == "" {
			return sdkgo.NewMutationBranch(success, NoteIdentity{ID: noteID}, nil, receipt)
		}
		return noteMutationBranch(operation, notFound, sdkgo.FailureNotFound, "Lever did not find the note", receipt)
	}
	if operation == "deleteNote" && response.status == http.StatusNoContent {
		return sdkgo.NewMutationBranch(success, NoteIdentity{ID: noteID}, nil, receipt)
	}
	if response.status != http.StatusOK {
		return noteMutationBranch(operation, rejected, sdkgo.FailureProviderRejection, "Lever rejected note write", receipt)
	}
	identity, err := decodeNoteIdentity(response.body)
	if err != nil || identity.ID != noteID {
		return sdkgo.NewMutationRetry[NoteIdentity](noteFailure(operation, sdkgo.FailureProtocol, "Lever did not confirm note write"), 0)
	}
	return sdkgo.NewMutationBranch(success, identity, nil, receipt)
}

func noteFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "lever", Operation: operation, Message: message}
}
func noteMutationBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string, receipt sdkgo.Receipt) sdkgo.MutationAttempt[NoteIdentity] {
	failure := noteFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, NoteIdentity{}, &failure, receipt)
}
