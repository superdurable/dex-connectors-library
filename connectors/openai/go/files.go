// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListFilesRequest selects one bounded page of stored files.
type ListFilesRequest struct {
	// Purpose filters files by their OpenAI purpose; blank lists all purposes.
	Purpose string `json:"purpose,omitempty"`
	// After is the previous page's last file ID; blank starts at the first page.
	After string `json:"after,omitempty"`
	// Limit is 1 through 100, or zero for 100 files.
	Limit int `json:"limit,omitempty"`
}

// FileIDRequest identifies one stored file.
type FileIDRequest struct {
	// FileID is returned by listFiles or shown in the OpenAI Platform Files page.
	FileID string `json:"fileId"`
}

// File contains safe metadata for a stored OpenAI file.
type File struct {
	// ID is the stable provider file ID.
	ID string `json:"id"`
	// Filename is the provider's file name.
	Filename string `json:"filename"`
	// Purpose is the file's OpenAI purpose.
	Purpose string `json:"purpose"`
	// Bytes is the file size, when known.
	Bytes *int64 `json:"bytes,omitempty"`
	// CreatedAt is the Unix timestamp in seconds.
	CreatedAt int64 `json:"createdAt"`
}

// FilePage is one provider page of file metadata.
type FilePage struct {
	// Files contains metadata, never file contents.
	Files []File `json:"files"`
	// HasMore indicates whether another page exists.
	HasMore bool `json:"hasMore"`
	// LastID is the cursor to pass as After for the next page.
	LastID string `json:"lastID"`
}

// ListFilesOperation lists file metadata.
type ListFilesOperation struct{ client *Client }

// DeleteFileOperation removes a file.
type DeleteFileOperation struct{ client *Client }

// ListFiles returns the bound file list Query.
func (client *Client) ListFiles() ListFilesOperation { return ListFilesOperation{client} }

// DeleteFile returns the bound file deletion Mutation.
func (client *Client) DeleteFile() DeleteFileOperation { return DeleteFileOperation{client} }

// Definition returns the manifest-generated file list contract.
func (ListFilesOperation) Definition() sdkgo.QueryDefinition { return ListFilesDefinition }

// Definition returns the manifest-generated file deletion contract.
func (DeleteFileOperation) Definition() sdkgo.MutationDefinition { return DeleteFileDefinition }

// IdempotencyKey uses the stable call ID for deletion correlation.
func (DeleteFileOperation) IdempotencyKey(callID sdkgo.CallID, _ FileIDRequest) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

func validFilePurpose(purpose string) bool {
	switch purpose {
	case "", "assistants", "assistants_output", "batch", "batch_output", "fine-tune", "fine-tune-results", "vision", "user_data":
		return true
	default:
		return false
	}
}

// Invoke lists one page of file metadata without reading file contents.
func (operation ListFilesOperation) Invoke(call sdkgo.Call, input ListFilesRequest) sdkgo.QueryAttempt[FilePage] {
	if !validFilePurpose(input.Purpose) || input.After != "" && !isSafeResponseID(input.After) || input.Limit < 0 || input.Limit > 100 {
		return sdkgo.NewQueryBranch(ListFilesBranchDefect, FilePage{}, conversationFailure(sdkgo.FailureValidation, "listFiles", "file page parameters are invalid"), sdkgo.Receipt{})
	}
	limit := input.Limit
	if limit == 0 {
		limit = 100
	}
	query := url.Values{"limit": {strconv.Itoa(limit)}}
	if input.Purpose != "" {
		query.Set("purpose", input.Purpose)
	}
	if input.After != "" {
		query.Set("after", input.After)
	}
	result := operation.client.exchangeConversation(call, call.Context, http.MethodGet, "/files?"+query.Encode(), nil)
	if result.requestFailure != nil {
		return sdkgo.NewQueryBranch(ListFilesBranchDefect, FilePage{}, result.requestFailure, sdkgo.Receipt{})
	}
	if result.err != nil || result.status == 408 || result.status == 429 || result.status >= 500 {
		return sdkgo.NewQueryRetry[FilePage](*conversationFailure(sdkgo.FailureAvailability, "listFiles", "file list is unavailable"), retryAfterHeader(result.header))
	}
	receipt := responseReceipt(call, result.requestID, result.header, "")
	if result.status != 200 {
		return sdkgo.NewQueryBranch(ListFilesBranchProviderRejected, FilePage{}, conversationFailure(sdkgo.FailureProviderRejection, "listFiles", "provider rejected file list"), receipt)
	}
	var wire struct {
		Object string `json:"object"`
		Data   []struct {
			ID        string `json:"id"`
			Object    string `json:"object"`
			Filename  string `json:"filename"`
			Purpose   string `json:"purpose"`
			Bytes     *int64 `json:"bytes"`
			CreatedAt int64  `json:"created_at"`
		} `json:"data"`
		HasMore bool   `json:"has_more"`
		LastID  string `json:"last_id"`
	}
	if json.Unmarshal(result.body, &wire) != nil || wire.Object != "list" || wire.Data == nil || wire.HasMore && !isSafeResponseID(wire.LastID) {
		return sdkgo.NewQueryBranch(ListFilesBranchInvalidResponse, FilePage{}, conversationFailure(sdkgo.FailureProtocol, "listFiles", "provider returned invalid file page"), receipt)
	}
	page := FilePage{Files: make([]File, 0, len(wire.Data)), HasMore: wire.HasMore, LastID: wire.LastID}
	for _, file := range wire.Data {
		if !isSafeResponseID(file.ID) || file.Object != "file" || file.Filename == "" || file.CreatedAt <= 0 || file.Bytes != nil && *file.Bytes < 0 {
			return sdkgo.NewQueryBranch(ListFilesBranchInvalidResponse, FilePage{}, conversationFailure(sdkgo.FailureProtocol, "listFiles", "provider returned invalid file metadata"), receipt)
		}
		page.Files = append(page.Files, File{ID: file.ID, Filename: file.Filename, Purpose: file.Purpose, Bytes: file.Bytes, CreatedAt: file.CreatedAt})
	}
	return sdkgo.NewQueryBranch(ListFilesBranchFound, page, nil, receipt)
}

// Invoke deletes one file, which OpenAI also removes from vector stores.
func (operation DeleteFileOperation) Invoke(call sdkgo.Call, input FileIDRequest) sdkgo.MutationAttempt[DeletedResponse] {
	if !isSafeResponseID(input.FileID) {
		return sdkgo.NewMutationBranch(DeleteFileBranchDefect, DeletedResponse{}, conversationFailure(sdkgo.FailureValidation, "deleteFile", "file ID is invalid"), sdkgo.Receipt{})
	}
	result := operation.client.exchangeConversation(call, call.Context, http.MethodDelete, "/files/"+url.PathEscape(input.FileID), nil)
	if result.requestFailure != nil {
		return sdkgo.NewMutationBranch(DeleteFileBranchDefect, DeletedResponse{}, result.requestFailure, sdkgo.Receipt{})
	}
	if result.err != nil || result.status == 408 || result.status == 429 || result.status >= 500 {
		return sdkgo.NewMutationRetry[DeletedResponse](*conversationFailure(sdkgo.FailureAvailability, "deleteFile", "file deletion outcome is unknown"), retryAfterHeader(result.header))
	}
	receipt := responseReceipt(call, result.requestID, result.header, input.FileID)
	if result.status == 404 {
		return sdkgo.NewMutationBranch(DeleteFileBranchNotFound, DeletedResponse{}, conversationFailure(sdkgo.FailureNotFound, "deleteFile", "file was not found"), receipt)
	}
	if result.status != 200 {
		return sdkgo.NewMutationBranch(DeleteFileBranchProviderRejected, DeletedResponse{}, conversationFailure(sdkgo.FailureProviderRejection, "deleteFile", "provider rejected file deletion"), receipt)
	}
	var wire struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Deleted bool   `json:"deleted"`
	}
	if json.Unmarshal(result.body, &wire) != nil || wire.ID != input.FileID || wire.Object != "file" || !wire.Deleted {
		return sdkgo.NewMutationBranch(DeleteFileBranchInvalidResponse, DeletedResponse{}, conversationFailure(sdkgo.FailureProtocol, "deleteFile", "provider returned invalid deletion receipt"), receipt)
	}
	return sdkgo.NewMutationBranch(DeleteFileBranchDeleted, DeletedResponse{ID: wire.ID, Deleted: true}, nil, receipt)
}
