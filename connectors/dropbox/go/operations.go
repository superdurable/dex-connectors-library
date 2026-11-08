// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dropbox

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListFolderInput selects one bounded page of a Dropbox folder.
type ListFolderInput struct {
	// Path is an empty string for the root or a Dropbox folder path or ID.
	Path string `json:"path"`
	// Limit is the approximate number of entries per page, from 1 to 2000.
	Limit int `json:"limit"`
}

// EntryMetadata contains a stable Dropbox entry identity without file contents.
type EntryMetadata struct {
	// ID is the Dropbox file or folder ID.
	ID string `json:"id"`
	// Name is the entry's current file or folder name.
	Name string `json:"name"`
	// PathDisplay is the provider's human-readable path.
	PathDisplay string `json:"pathDisplay"`
	// Kind is file or folder.
	Kind string `json:"kind"`
}

// FolderPage contains a bounded folder listing and its continuation cursor.
type FolderPage struct {
	// Entries contains the page's file and folder identities.
	Entries []EntryMetadata `json:"entries"`
	// Cursor is passed to a future continuation request when HasMore is true.
	Cursor string `json:"cursor"`
	// HasMore reports whether Dropbox has more entries for this cursor.
	HasMore bool `json:"hasMore"`
}

// GetMetadataInput selects one Dropbox file or folder.
type GetMetadataInput struct {
	// Path is an absolute Dropbox path or a stable id: or rev: identifier.
	Path string `json:"path"`
}

// ContinueFolderInput selects the page or changes after a Dropbox cursor.
type ContinueFolderInput struct {
	// Cursor is returned by a previous folder listing or continuation.
	Cursor string `json:"cursor"`
}

// ListFolderOperation reads one folder page.
type ListFolderOperation struct{ client *Client }

// GetMetadataOperation reads one file or folder identity.
type GetMetadataOperation struct{ client *Client }

// ContinueFolderOperation reads the next folder page or change set.
type ContinueFolderOperation struct{ client *Client }

// ListFolder binds folder discovery to this client.
func (client *Client) ListFolder() ListFolderOperation { return ListFolderOperation{client} }

// GetMetadata binds entry metadata lookup to this client.
func (client *Client) GetMetadata() GetMetadataOperation { return GetMetadataOperation{client} }

// ContinueFolder binds cursor continuation to this client.
func (client *Client) ContinueFolder() ContinueFolderOperation {
	return ContinueFolderOperation{client}
}

// Definition returns the generated folder Query contract.
func (ListFolderOperation) Definition() sdkgo.QueryDefinition { return ListFolderDefinition }

// Definition returns the generated entry Query contract.
func (GetMetadataOperation) Definition() sdkgo.QueryDefinition { return GetMetadataDefinition }

// Definition returns the generated continuation Query contract.
func (ContinueFolderOperation) Definition() sdkgo.QueryDefinition { return ContinueFolderDefinition }

// Invoke lists one page of direct child entries.
func (operation ListFolderOperation) Invoke(call sdkgo.Call, input ListFolderInput) sdkgo.QueryAttempt[FolderPage] {
	branches := queryBranches{found: ListFolderBranchListed, notFound: ListFolderBranchNotFound, rejected: ListFolderBranchProviderRejected, invalid: ListFolderBranchInvalidResponse, defect: ListFolderBranchDefect}
	if !validDropboxPath(input.Path, true) || input.Limit < 1 || input.Limit > 2000 {
		return queryBranch("listFolder", branches.defect, FolderPage{}, sdkgo.FailureValidation, "folder path or page limit is invalid")
	}
	payload, err := json.Marshal(struct {
		Path  string `json:"path"`
		Limit int    `json:"limit"`
	}{Path: input.Path, Limit: input.Limit})
	if err != nil {
		return queryBranch("listFolder", branches.defect, FolderPage{}, sdkgo.FailureLocalDefect, "folder request could not be built")
	}
	return invokeQuery(call, operation.client, "listFolder", "/2/files/list_folder", payload, branches, decodeFolderPage)
}

// Invoke reads one Dropbox file or folder identity.
func (operation GetMetadataOperation) Invoke(call sdkgo.Call, input GetMetadataInput) sdkgo.QueryAttempt[EntryMetadata] {
	branches := queryBranches{found: GetMetadataBranchFound, notFound: GetMetadataBranchNotFound, rejected: GetMetadataBranchProviderRejected, invalid: GetMetadataBranchInvalidResponse, defect: GetMetadataBranchDefect}
	if !validDropboxPath(input.Path, false) {
		return queryBranch("getMetadata", branches.defect, EntryMetadata{}, sdkgo.FailureValidation, "file or folder path is invalid")
	}
	payload, err := json.Marshal(struct {
		Path string `json:"path"`
	}{Path: input.Path})
	if err != nil {
		return queryBranch("getMetadata", branches.defect, EntryMetadata{}, sdkgo.FailureLocalDefect, "metadata request could not be built")
	}
	return invokeQuery(call, operation.client, "getMetadata", "/2/files/get_metadata", payload, branches, decodeEntry)
}

// Invoke continues a Dropbox folder listing from its last returned cursor.
func (operation ContinueFolderOperation) Invoke(call sdkgo.Call, input ContinueFolderInput) sdkgo.QueryAttempt[FolderPage] {
	branches := queryBranches{found: ContinueFolderBranchListed, cursorExpired: ContinueFolderBranchCursorExpired, rejected: ContinueFolderBranchProviderRejected, invalid: ContinueFolderBranchInvalidResponse, defect: ContinueFolderBranchDefect}
	if input.Cursor == "" || len(input.Cursor) > 4096 || !utf8.ValidString(input.Cursor) || strings.ContainsAny(input.Cursor, "\r\n\x00") {
		return queryBranch("continueFolder", branches.defect, FolderPage{}, sdkgo.FailureValidation, "folder cursor is invalid")
	}
	payload, err := json.Marshal(struct {
		Cursor string `json:"cursor"`
	}{Cursor: input.Cursor})
	if err != nil {
		return queryBranch("continueFolder", branches.defect, FolderPage{}, sdkgo.FailureLocalDefect, "continuation request could not be built")
	}
	return invokeQuery(call, operation.client, "continueFolder", "/2/files/list_folder/continue", payload, branches, decodeFolderPage)
}

func validDropboxPath(path string, allowRoot bool) bool {
	if path == "" {
		return allowRoot
	}
	if len(path) > 4096 || !utf8.ValidString(path) {
		return false
	}
	for _, character := range path {
		if character < 32 || character == 127 {
			return false
		}
	}
	return strings.HasPrefix(path, "/") || strings.HasPrefix(path, "id:") || strings.HasPrefix(path, "ns:") || strings.HasPrefix(path, "rev:")
}

func decodeFolderPage(body []byte) (FolderPage, error) {
	var wire struct {
		Entries []json.RawMessage `json:"entries"`
		Cursor  string            `json:"cursor"`
		HasMore *bool             `json:"has_more"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return FolderPage{}, err
	}
	if wire.Entries == nil || wire.Cursor == "" || wire.HasMore == nil {
		return FolderPage{}, fmt.Errorf("folder page is incomplete")
	}
	entries := make([]EntryMetadata, 0, len(wire.Entries))
	for _, raw := range wire.Entries {
		entry, err := decodeEntry(raw)
		if err != nil {
			return FolderPage{}, err
		}
		entries = append(entries, entry)
	}
	return FolderPage{Entries: entries, Cursor: wire.Cursor, HasMore: *wire.HasMore}, nil
}

func decodeEntry(body []byte) (EntryMetadata, error) {
	var wire struct {
		Tag         string `json:".tag"`
		ID          string `json:"id"`
		Name        string `json:"name"`
		PathDisplay string `json:"path_display"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return EntryMetadata{}, err
	}
	if wire.Tag != "file" && wire.Tag != "folder" {
		return EntryMetadata{}, fmt.Errorf("entry kind is invalid")
	}
	if wire.ID == "" || wire.Name == "" || wire.PathDisplay == "" {
		return EntryMetadata{}, fmt.Errorf("entry identity is incomplete")
	}
	return EntryMetadata{ID: wire.ID, Name: wire.Name, PathDisplay: wire.PathDisplay, Kind: wire.Tag}, nil
}
