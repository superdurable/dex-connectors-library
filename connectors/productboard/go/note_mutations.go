// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package productboard

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// RenameNoteInput replaces one note's name.
type RenameNoteInput struct {
	// NoteID is the UUID of the note to rename.
	NoteID string `json:"noteId"`
	// Name is the new nonempty title.
	Name string `json:"name"`
}

// RenamedNote confirms the ID and requested absolute name.
type RenamedNote struct {
	// ID is the Productboard note UUID.
	ID string `json:"id"`
	// Name is the new note title.
	Name string `json:"name"`
}

// ArchiveNoteInput identifies the note to archive.
type ArchiveNoteInput struct {
	// NoteID is the UUID of the note to archive.
	NoteID string `json:"noteId"`
}

// ArchivedNote confirms the archived note ID.
type ArchivedNote struct {
	// ID is the Productboard note UUID.
	ID string `json:"id"`
}

// DeleteNoteInput identifies the note to delete permanently.
type DeleteNoteInput struct {
	// NoteID is the UUID of the note to delete.
	NoteID string `json:"noteId"`
}

// DeletedNote confirms the deleted note ID.
type DeletedNote struct {
	// ID is the Productboard note UUID.
	ID string `json:"id"`
}

// RenameNoteOperation replaces a note name with a repeat-safe value.
type RenameNoteOperation struct{ client *Client }

// RenameNote binds the Mutation to this client.
func (client *Client) RenameNote() RenameNoteOperation { return RenameNoteOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (RenameNoteOperation) Definition() sdkgo.MutationDefinition { return RenameNoteDefinition }

// IdempotencyKey identifies retries of the same rename.
func (RenameNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ RenameNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces a note's name through Productboard's fields update.
func (operation RenameNoteOperation) Invoke(call sdkgo.Call, input RenameNoteInput) sdkgo.MutationAttempt[RenamedNote] {
	if !validFeatureID(input.NoteID) || !validFeatureName(input.Name) {
		return featureMutationBranch(RenameNoteBranchDefect, "renameNote", sdkgo.FailureValidation, "note ID or name is invalid", RenamedNote{})
	}
	payload, _ := json.Marshal(map[string]any{"data": map[string]any{"fields": map[string]string{"name": input.Name}}})
	response := operation.client.request(call, http.MethodPatch, "/notes/"+url.PathEscape(input.NoteID), payload)
	if errors.Is(response.err, errCredential) {
		return featureMutationBranch(RenameNoteBranchDefect, "renameNote", sdkgo.FailureAuthentication, "Productboard credentials are unavailable", RenamedNote{})
	}
	if response.status == http.StatusNotFound {
		return featureMutationBranch(RenameNoteBranchNotFound, "renameNote", sdkgo.FailureNotFound, "note was not found", RenamedNote{})
	}
	if kind, retry := featureRetryKind(response); retry {
		return sdkgo.NewMutationRetry[RenamedNote](featureFailure("renameNote", kind, "Productboard note rename is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return featureMutationBranch(RenameNoteBranchProviderRejected, "renameNote", sdkgo.FailureProviderRejection, "Productboard rejected the note rename", RenamedNote{})
	}
	if response.err != nil {
		return featureMutationBranch(RenameNoteBranchInvalidResponse, "renameNote", sdkgo.FailureProtocol, "Productboard note response could not be read", RenamedNote{})
	}
	noteID, valid := decodeUpdatedNoteID(response.body)
	if !valid || noteID != input.NoteID {
		return featureMutationBranch(RenameNoteBranchInvalidResponse, "renameNote", sdkgo.FailureProtocol, "Productboard returned an invalid note ID", RenamedNote{})
	}
	return sdkgo.NewMutationBranch(RenameNoteBranchRenamed, RenamedNote{ID: noteID, Name: input.Name}, nil, sdkgo.Receipt{})
}

// ArchiveNoteOperation sets archived=true without deleting a note.
type ArchiveNoteOperation struct{ client *Client }

// ArchiveNote binds the Mutation to this client.
func (client *Client) ArchiveNote() ArchiveNoteOperation { return ArchiveNoteOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (ArchiveNoteOperation) Definition() sdkgo.MutationDefinition { return ArchiveNoteDefinition }

// IdempotencyKey identifies retries of the same archive.
func (ArchiveNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ ArchiveNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets archived to true and returns only the note ID.
func (operation ArchiveNoteOperation) Invoke(call sdkgo.Call, input ArchiveNoteInput) sdkgo.MutationAttempt[ArchivedNote] {
	if !validFeatureID(input.NoteID) {
		return featureMutationBranch(ArchiveNoteBranchDefect, "archiveNote", sdkgo.FailureValidation, "note ID must be a UUID", ArchivedNote{})
	}
	payload, _ := json.Marshal(map[string]any{"data": map[string]any{"fields": map[string]bool{"archived": true}}})
	response := operation.client.request(call, http.MethodPatch, "/notes/"+url.PathEscape(input.NoteID), payload)
	if errors.Is(response.err, errCredential) {
		return featureMutationBranch(ArchiveNoteBranchDefect, "archiveNote", sdkgo.FailureAuthentication, "Productboard credentials are unavailable", ArchivedNote{})
	}
	if response.status == http.StatusNotFound {
		return featureMutationBranch(ArchiveNoteBranchNotFound, "archiveNote", sdkgo.FailureNotFound, "note was not found", ArchivedNote{})
	}
	if kind, retry := featureRetryKind(response); retry {
		return sdkgo.NewMutationRetry[ArchivedNote](featureFailure("archiveNote", kind, "Productboard note archive is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return featureMutationBranch(ArchiveNoteBranchProviderRejected, "archiveNote", sdkgo.FailureProviderRejection, "Productboard rejected the note archive", ArchivedNote{})
	}
	if response.err != nil {
		return featureMutationBranch(ArchiveNoteBranchInvalidResponse, "archiveNote", sdkgo.FailureProtocol, "Productboard note response could not be read", ArchivedNote{})
	}
	noteID, valid := decodeUpdatedNoteID(response.body)
	if !valid || noteID != input.NoteID {
		return featureMutationBranch(ArchiveNoteBranchInvalidResponse, "archiveNote", sdkgo.FailureProtocol, "Productboard returned an invalid note ID", ArchivedNote{})
	}
	return sdkgo.NewMutationBranch(ArchiveNoteBranchArchived, ArchivedNote{ID: noteID}, nil, sdkgo.Receipt{})
}

// DeleteNoteOperation permanently deletes one note and its relationships.
type DeleteNoteOperation struct{ client *Client }

// DeleteNote binds the Mutation to this client.
func (client *Client) DeleteNote() DeleteNoteOperation { return DeleteNoteOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (DeleteNoteOperation) Definition() sdkgo.MutationDefinition { return DeleteNoteDefinition }

// IdempotencyKey identifies retries of the same delete.
func (DeleteNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes one note and returns its stable ID.
func (operation DeleteNoteOperation) Invoke(call sdkgo.Call, input DeleteNoteInput) sdkgo.MutationAttempt[DeletedNote] {
	if !validFeatureID(input.NoteID) {
		return featureMutationBranch(DeleteNoteBranchDefect, "deleteNote", sdkgo.FailureValidation, "note ID must be a UUID", DeletedNote{})
	}
	response := operation.client.request(call, http.MethodDelete, "/notes/"+url.PathEscape(input.NoteID), nil)
	if errors.Is(response.err, errCredential) {
		return featureMutationBranch(DeleteNoteBranchDefect, "deleteNote", sdkgo.FailureAuthentication, "Productboard credentials are unavailable", DeletedNote{})
	}
	if response.status == http.StatusNotFound {
		return featureMutationBranch(DeleteNoteBranchNotFound, "deleteNote", sdkgo.FailureNotFound, "note was not found", DeletedNote{})
	}
	if kind, retry := featureRetryKind(response); retry {
		return sdkgo.NewMutationRetry[DeletedNote](featureFailure("deleteNote", kind, "Productboard note deletion is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusNoContent || response.err != nil {
		return featureMutationBranch(DeleteNoteBranchProviderRejected, "deleteNote", sdkgo.FailureProviderRejection, "Productboard rejected the note delete", DeletedNote{})
	}
	return sdkgo.NewMutationBranch(DeleteNoteBranchDeleted, DeletedNote{ID: input.NoteID}, nil, sdkgo.Receipt{})
}

func decodeUpdatedNoteID(body []byte) (string, bool) {
	var wire struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || !validFeatureID(wire.Data.ID) {
		return "", false
	}
	return wire.Data.ID, true
}
