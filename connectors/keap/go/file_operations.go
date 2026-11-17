// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package keap

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListFilesInput selects a bounded page of file metadata.
type ListFilesInput struct {
	// PageSize is the maximum files to return; zero uses 20.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is an opaque continuation token; blank starts the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// FileIDInput selects a Keap file by ID.
type FileIDInput struct {
	// FileID is the Keap file ID.
	FileID string `json:"fileId"`
}

// FileMetadata contains a file's identifying metadata, not its contents.
type FileMetadata struct {
	// ID is Keap's file ID.
	ID string `json:"id"`
	// FileName is the filename with extension.
	FileName string `json:"fileName"`
	// FileSizeBytes is the file size in bytes.
	FileSizeBytes int64 `json:"fileSizeBytes"`
	// IsPublic reports Keap's public-access flag.
	IsPublic bool `json:"isPublic"`
}

// FilesPage contains a page of file metadata and a continuation token.
type FilesPage struct {
	// Files are the returned file metadata records.
	Files []FileMetadata `json:"files"`
	// NextPageToken selects the next page, when present.
	NextPageToken string `json:"nextPageToken"`
}

// FileDeletion confirms deletion of one Keap file.
type FileDeletion struct {
	// FileID is the deleted file ID.
	FileID string `json:"fileId"`
	// Deleted reports Keap's acceptance of deletion.
	Deleted bool `json:"deleted"`
}

// ListFilesOperation reads a page of file metadata.
type ListFilesOperation struct{ client *Client }

// DeleteFileOperation deletes one file.
type DeleteFileOperation struct{ client *Client }

// ListFiles binds file metadata listing to the client.
func (client *Client) ListFiles() ListFilesOperation { return ListFilesOperation{client} }

// DeleteFile binds file deletion to the client.
func (client *Client) DeleteFile() DeleteFileOperation { return DeleteFileOperation{client} }

// Definition returns the generated query contract.
func (ListFilesOperation) Definition() sdkgo.QueryDefinition { return ListFilesDefinition }

// Definition returns the generated mutation contract.
func (DeleteFileOperation) Definition() sdkgo.MutationDefinition { return DeleteFileDefinition }

// IdempotencyKey derives the stable Dex call identity.
func (DeleteFileOperation) IdempotencyKey(callID sdkgo.CallID, _ FileIDInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one bounded page of file metadata.
func (operation ListFilesOperation) Invoke(call sdkgo.Call, input ListFilesInput) sdkgo.QueryAttempt[FilesPage] {
	if input.PageSize < 0 || input.PageSize > 100 || len(input.PageToken) > 2048 {
		return keapQueryBranch("listFiles", ListFilesBranchDefect, FilesPage{}, sdkgo.FailureValidation, "page size or token is invalid")
	}
	if input.PageSize == 0 {
		input.PageSize = 20
	}
	query := url.Values{"page_size": {strconv.Itoa(input.PageSize)}}
	if input.PageToken != "" {
		query.Set("page_token", input.PageToken)
	}
	response := operation.client.request(call, http.MethodGet, "/rest/v2/files?"+query.Encode(), nil)
	if branch, retry, handled := classifyKeapQuery(response, "listFiles", ListFilesBranchProviderRejected, ListFilesBranchInvalidResponse, ListFilesBranchDefect, FilesPage{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	var wire struct {
		Files         []json.RawMessage `json:"files"`
		NextPageToken string            `json:"next_page_token"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Files == nil {
		return keapQueryBranch("listFiles", ListFilesBranchInvalidResponse, FilesPage{}, sdkgo.FailureProtocol, "Keap returned invalid file list JSON")
	}
	page := FilesPage{Files: make([]FileMetadata, 0, len(wire.Files)), NextPageToken: wire.NextPageToken}
	for _, raw := range wire.Files {
		file, ok := decodeFileMetadata(raw)
		if !ok {
			return keapQueryBranch("listFiles", ListFilesBranchInvalidResponse, FilesPage{}, sdkgo.FailureProtocol, "Keap returned invalid file metadata")
		}
		page.Files = append(page.Files, file)
	}
	return sdkgo.NewQueryBranch(ListFilesBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke deletes one Keap file.
func (operation DeleteFileOperation) Invoke(call sdkgo.Call, input FileIDInput) sdkgo.MutationAttempt[FileDeletion] {
	if !validContactID(input.FileID) {
		return keapMutationBranch("deleteFile", DeleteFileBranchDefect, FileDeletion{}, sdkgo.FailureValidation, "file ID is invalid")
	}
	response := operation.client.request(call, http.MethodDelete, "/rest/v2/files/"+input.FileID, nil)
	if response.status == 404 {
		return keapMutationBranch("deleteFile", DeleteFileBranchNotFound, FileDeletion{}, sdkgo.FailureNotFound, "Keap did not find the file")
	}
	if branch, retry, handled := classifyKeapMutation(response, "deleteFile", DeleteFileBranchProviderRejected, DeleteFileBranchInvalidResponse, DeleteFileBranchDefect, FileDeletion{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	if response.status != 200 && response.status != 204 {
		return keapMutationBranch("deleteFile", DeleteFileBranchProviderRejected, FileDeletion{}, sdkgo.FailureProviderRejection, "Keap rejected file deletion")
	}
	if response.status == 200 && len(response.body) > 0 && !json.Valid(response.body) {
		return keapMutationBranch("deleteFile", DeleteFileBranchInvalidResponse, FileDeletion{}, sdkgo.FailureProtocol, "Keap returned invalid deletion JSON")
	}
	return sdkgo.NewMutationBranch(DeleteFileBranchDeleted, FileDeletion{FileID: input.FileID, Deleted: true}, nil, sdkgo.Receipt{})
}

func decodeFileMetadata(body []byte) (FileMetadata, bool) {
	var wire struct {
		ID       string `json:"id"`
		FileName string `json:"file_name"`
		FileSize int64  `json:"file_size"`
		IsPublic bool   `json:"is_public"`
	}
	if json.Unmarshal(body, &wire) != nil || !validContactID(wire.ID) || wire.FileSize < 0 {
		return FileMetadata{}, false
	}
	return FileMetadata{ID: wire.ID, FileName: wire.FileName, FileSizeBytes: wire.FileSize, IsPublic: wire.IsPublic}, true
}
