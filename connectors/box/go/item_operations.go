// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package box

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetFileInput selects one Box file by its decimal ID.
type GetFileInput struct {
	// FileID appears in Box Web's /files/<ID> URL.
	FileID string `json:"fileId"`
}

// GetFolderInput selects one Box folder by its decimal ID.
type GetFolderInput struct {
	// FolderID appears in Box Web's /folders/<ID> URL; 0 is the root folder.
	FolderID string `json:"folderId"`
}

// ListFolderItemsInput selects one offset page in a Box folder.
type ListFolderItemsInput struct {
	// FolderID appears in Box Web's /folders/<ID> URL; 0 is the root folder.
	FolderID string `json:"folderId"`
	// Limit is 1 through 1000 items; zero uses Box's default.
	Limit int `json:"limit,omitempty"`
	// Offset is the zero-based page offset.
	Offset int `json:"offset,omitempty"`
}

// SearchFilesInput searches accessible files by name or content.
type SearchFilesInput struct {
	// Query is Box search text; blank is invalid.
	Query string `json:"query"`
	// Limit is 1 through 200 results; zero uses Box's default.
	Limit int `json:"limit,omitempty"`
	// Offset is the zero-based result offset.
	Offset int `json:"offset,omitempty"`
}

// SearchFoldersInput searches accessible folders by name.
type SearchFoldersInput struct {
	// Query is Box search text; blank is invalid.
	Query string `json:"query"`
	// Limit is 1 through 200 results; zero uses Box's default.
	Limit int `json:"limit,omitempty"`
	// Offset is the zero-based result offset.
	Offset int `json:"offset,omitempty"`
}

// DeleteFileInput selects one Box file for deletion.
type DeleteFileInput struct {
	// FileID appears in Box Web's /files/<ID> URL.
	FileID string `json:"fileId"`
}

// DeleteFolderInput selects one empty Box folder for deletion.
type DeleteFolderInput struct {
	// FolderID appears in Box Web's /folders/<ID> URL; root is not deletable.
	FolderID string `json:"folderId"`
}

// BoxFile contains selected metadata for one Box file.
type BoxFile struct {
	// ID is the stable Box file ID.
	ID string `json:"id"`
	// Type is always file.
	Type string `json:"type"`
	// Name is the current file name.
	Name string `json:"name"`
	// Size is the file size in bytes.
	Size int64 `json:"size"`
	// SHA1 is the provider content digest when available.
	SHA1 string `json:"sha1"`
}

// BoxFolder contains selected metadata for one Box folder.
type BoxFolder struct {
	// ID is the stable Box folder ID.
	ID string `json:"id"`
	// Type is always folder.
	Type string `json:"type"`
	// Name is the current folder name.
	Name string `json:"name"`
}

// BoxItem contains selected metadata for one folder or search result.
type BoxItem struct {
	// ID is the stable Box item ID.
	ID string `json:"id"`
	// Type is file, folder, or web_link.
	Type string `json:"type"`
	// Name is the current item name.
	Name string `json:"name"`
}

// BoxItemPage contains one offset page of Box items.
type BoxItemPage struct {
	// Entries are the items in this page.
	Entries []BoxItem `json:"entries"`
	// Limit is the provider page size.
	Limit int `json:"limit"`
	// Offset is the zero-based provider page offset.
	Offset int `json:"offset"`
	// TotalCount is Box's total-count bound for this listing.
	TotalCount int `json:"total_count"`
}

// DeletedBoxItem records an item ID after Box accepts deletion.
type DeletedBoxItem struct {
	// ID is the file or folder ID accepted by Box.
	ID string `json:"id"`
}

// GetFileOperation retrieves one file's metadata.
type GetFileOperation struct{ client *Client }

// GetFolderOperation retrieves one folder's metadata.
type GetFolderOperation struct{ client *Client }

// ListFolderItemsOperation lists one folder's children.
type ListFolderItemsOperation struct{ client *Client }

// SearchFilesOperation searches accessible files.
type SearchFilesOperation struct{ client *Client }

// SearchFoldersOperation searches accessible folders.
type SearchFoldersOperation struct{ client *Client }

// DeleteFileOperation removes one Box file.
type DeleteFileOperation struct{ client *Client }

// DeleteFolderOperation removes one empty Box folder.
type DeleteFolderOperation struct{ client *Client }

// GetFile binds the file metadata Query to this client.
func (client *Client) GetFile() GetFileOperation { return GetFileOperation{client} }

// GetFolder binds the folder metadata Query to this client.
func (client *Client) GetFolder() GetFolderOperation { return GetFolderOperation{client} }

// ListFolderItems binds the folder listing Query to this client.
func (client *Client) ListFolderItems() ListFolderItemsOperation {
	return ListFolderItemsOperation{client}
}

// SearchFiles binds the file search Query to this client.
func (client *Client) SearchFiles() SearchFilesOperation { return SearchFilesOperation{client} }

// SearchFolders binds the folder search Query to this client.
func (client *Client) SearchFolders() SearchFoldersOperation { return SearchFoldersOperation{client} }

// DeleteFile binds the file deletion Mutation to this client.
func (client *Client) DeleteFile() DeleteFileOperation { return DeleteFileOperation{client} }

// DeleteFolder binds the folder deletion Mutation to this client.
func (client *Client) DeleteFolder() DeleteFolderOperation { return DeleteFolderOperation{client} }

// Definition returns the generated getFile contract.
func (GetFileOperation) Definition() sdkgo.QueryDefinition { return GetFileDefinition }

// Definition returns the generated getFolder contract.
func (GetFolderOperation) Definition() sdkgo.QueryDefinition { return GetFolderDefinition }

// Definition returns the generated listFolderItems contract.
func (ListFolderItemsOperation) Definition() sdkgo.QueryDefinition { return ListFolderItemsDefinition }

// Definition returns the generated searchFiles contract.
func (SearchFilesOperation) Definition() sdkgo.QueryDefinition { return SearchFilesDefinition }

// Definition returns the generated searchFolders contract.
func (SearchFoldersOperation) Definition() sdkgo.QueryDefinition { return SearchFoldersDefinition }

// Definition returns the generated deleteFile contract.
func (DeleteFileOperation) Definition() sdkgo.MutationDefinition { return DeleteFileDefinition }

// Definition returns the generated deleteFolder contract.
func (DeleteFolderOperation) Definition() sdkgo.MutationDefinition { return DeleteFolderDefinition }

// IdempotencyKey derives a stable call key for an absolute file deletion.
func (DeleteFileOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteFileInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable call key for an absolute folder deletion.
func (DeleteFolderOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteFolderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one file's metadata and classifies provider outcomes.
func (operation GetFileOperation) Invoke(call sdkgo.Call, input GetFileInput) sdkgo.QueryAttempt[BoxFile] {
	if !validBoxItemID(input.FileID, false) {
		return queryBranch("getFile", GetFileBranchDefect, BoxFile{}, sdkgo.FailureValidation, "Box file ID must contain decimal digits")
	}
	path := "/2.0/files/" + url.PathEscape(input.FileID) + "?fields=id,type,name,sha1,size"
	return runBoxItemQuery(operation.client, call, "getFile", path, GetFileBranchFound, GetFileBranchNotFound, GetFileBranchProviderRejected, GetFileBranchInvalidResponse, GetFileBranchDefect, func(body []byte) (BoxFile, error) {
		var file BoxFile
		err := json.Unmarshal(body, &file)
		if err == nil && (file.ID != input.FileID || file.Type != "file" || file.Name == "" || file.Size < 0) {
			err = fmt.Errorf("Box file metadata is invalid")
		}
		return file, err
	})
}

// Invoke reads one folder's metadata and classifies provider outcomes.
func (operation GetFolderOperation) Invoke(call sdkgo.Call, input GetFolderInput) sdkgo.QueryAttempt[BoxFolder] {
	if !validBoxItemID(input.FolderID, true) {
		return queryBranch("getFolder", GetFolderBranchDefect, BoxFolder{}, sdkgo.FailureValidation, "Box folder ID must contain decimal digits")
	}
	path := "/2.0/folders/" + url.PathEscape(input.FolderID) + "?fields=id,type,name"
	return runBoxItemQuery(operation.client, call, "getFolder", path, GetFolderBranchFound, GetFolderBranchNotFound, GetFolderBranchProviderRejected, GetFolderBranchInvalidResponse, GetFolderBranchDefect, func(body []byte) (BoxFolder, error) {
		var folder BoxFolder
		err := json.Unmarshal(body, &folder)
		if err == nil && (folder.ID != input.FolderID || folder.Type != "folder" || folder.Name == "") {
			err = fmt.Errorf("Box folder metadata is invalid")
		}
		return folder, err
	})
}

// Invoke lists one offset page in a Box folder.
func (operation ListFolderItemsOperation) Invoke(call sdkgo.Call, input ListFolderItemsInput) sdkgo.QueryAttempt[BoxItemPage] {
	if !validBoxItemID(input.FolderID, true) || !validBoxPage(input.Limit, input.Offset, 1000) {
		return queryBranch("listFolderItems", ListFolderItemsBranchDefect, BoxItemPage{}, sdkgo.FailureValidation, "Box folder ID or page bounds are invalid")
	}
	query := boxPageQuery(input.Limit, input.Offset)
	path := "/2.0/folders/" + url.PathEscape(input.FolderID) + "/items" + query
	return runBoxItemQuery(operation.client, call, "listFolderItems", path, ListFolderItemsBranchListed, ListFolderItemsBranchNotFound, ListFolderItemsBranchProviderRejected, ListFolderItemsBranchInvalidResponse, ListFolderItemsBranchDefect, decodeBoxItemPage)
}

// Invoke searches Box files by text query.
func (operation SearchFilesOperation) Invoke(call sdkgo.Call, input SearchFilesInput) sdkgo.QueryAttempt[BoxItemPage] {
	if strings.TrimSpace(input.Query) == "" || !validBoxPage(input.Limit, input.Offset, 200) {
		return queryBranch("searchFiles", SearchFilesBranchDefect, BoxItemPage{}, sdkgo.FailureValidation, "Box search query or page bounds are invalid")
	}
	path := boxSearchPath(input.Query, "file", input.Limit, input.Offset)
	return runBoxItemQuery(operation.client, call, "searchFiles", path, SearchFilesBranchFound, "", SearchFilesBranchProviderRejected, SearchFilesBranchInvalidResponse, SearchFilesBranchDefect, decodeBoxItemPage)
}

// Invoke searches Box folders by text query.
func (operation SearchFoldersOperation) Invoke(call sdkgo.Call, input SearchFoldersInput) sdkgo.QueryAttempt[BoxItemPage] {
	if strings.TrimSpace(input.Query) == "" || !validBoxPage(input.Limit, input.Offset, 200) {
		return queryBranch("searchFolders", SearchFoldersBranchDefect, BoxItemPage{}, sdkgo.FailureValidation, "Box search query or page bounds are invalid")
	}
	path := boxSearchPath(input.Query, "folder", input.Limit, input.Offset)
	return runBoxItemQuery(operation.client, call, "searchFolders", path, SearchFoldersBranchFound, "", SearchFoldersBranchProviderRejected, SearchFoldersBranchInvalidResponse, SearchFoldersBranchDefect, decodeBoxItemPage)
}

// Invoke deletes one Box file; Box may move it to trash or remove it permanently.
func (operation DeleteFileOperation) Invoke(call sdkgo.Call, input DeleteFileInput) sdkgo.MutationAttempt[DeletedBoxItem] {
	if !validBoxItemID(input.FileID, false) {
		return boxMutationFailure("deleteFile", DeleteFileBranchDefect, sdkgo.FailureValidation, "Box file ID must contain decimal digits")
	}
	return runBoxDelete(operation.client, call, "deleteFile", "/2.0/files/"+url.PathEscape(input.FileID), input.FileID, DeleteFileBranchDeleted, DeleteFileBranchNotFound, DeleteFileBranchProviderRejected, DeleteFileBranchInvalidResponse, DeleteFileBranchDefect)
}

// Invoke deletes one empty Box folder; Box may move it to trash or remove it permanently.
func (operation DeleteFolderOperation) Invoke(call sdkgo.Call, input DeleteFolderInput) sdkgo.MutationAttempt[DeletedBoxItem] {
	if !validBoxItemID(input.FolderID, false) {
		return boxMutationFailure("deleteFolder", DeleteFolderBranchDefect, sdkgo.FailureValidation, "Box folder ID must contain decimal digits and cannot be root")
	}
	return runBoxDelete(operation.client, call, "deleteFolder", "/2.0/folders/"+url.PathEscape(input.FolderID), input.FolderID, DeleteFolderBranchDeleted, DeleteFolderBranchNotFound, DeleteFolderBranchProviderRejected, DeleteFolderBranchInvalidResponse, DeleteFolderBranchDefect)
}

func (client *Client) requestBoxItem(call sdkgo.Call, method, path string) fileResponse {
	accessToken, err := client.resolveAccessToken(call)
	if err != nil {
		return fileResponse{err: err}
	}
	request, err := http.NewRequestWithContext(call.Context, method, client.baseURL+path, nil)
	if err != nil {
		return fileResponse{err: errRequest}
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	return client.sendMetadataRequest(request, accessToken)
}

func runBoxItemQuery[T any](client *Client, call sdkgo.Call, operation, path string, success, notFound, rejected, invalid, defect sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	response := client.requestBoxItem(call, http.MethodGet, path)
	if errors.Is(response.err, errCredential) {
		return queryBranch(operation, defect, *new(T), sdkgo.FailureAuthentication, "Box credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRefresh) || errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[T](queryFailure(operation, sdkgo.FailureAvailability, "Box provider request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewQueryRetry[T](queryFailure(operation, kind, "Box provider is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound && notFound != "" {
		return queryBranch(operation, notFound, *new(T), sdkgo.FailureNotFound, "Box item was not found")
	}
	if response.status != http.StatusOK {
		return queryBranch(operation, rejected, *new(T), sdkgo.FailureProviderRejection, "Box rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return queryBranch(operation, invalid, *new(T), sdkgo.FailureResponseTooLarge, "Box response exceeds the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[T](queryFailure(operation, sdkgo.FailureTransport, "Box response was interrupted"), 0)
	}
	output, err := decode(response.body)
	if err != nil {
		return queryBranch(operation, invalid, *new(T), sdkgo.FailureProtocol, "Box response is incomplete or malformed")
	}
	return sdkgo.NewQueryBranch(success, output, nil, sdkgo.Receipt{})
}

func runBoxDelete(client *Client, call sdkgo.Call, operation, path, itemID string, success, notFound, rejected, invalid, defect sdkgo.BranchID) sdkgo.MutationAttempt[DeletedBoxItem] {
	response := client.requestBoxItem(call, http.MethodDelete, path)
	if errors.Is(response.err, errCredential) {
		return boxMutationFailure(operation, defect, sdkgo.FailureAuthentication, "Box credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRefresh) || errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[DeletedBoxItem](queryFailure(operation, sdkgo.FailureAvailability, "Box provider request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[DeletedBoxItem](queryFailure(operation, kind, "Box provider is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return boxMutationFailure(operation, notFound, sdkgo.FailureNotFound, "Box item was not found")
	}
	if response.status != http.StatusNoContent {
		return boxMutationFailure(operation, rejected, sdkgo.FailureProviderRejection, "Box rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) || len(response.body) != 0 {
		return boxMutationFailure(operation, invalid, sdkgo.FailureProtocol, "Box returned unexpected deletion content")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[DeletedBoxItem](queryFailure(operation, sdkgo.FailureTransport, "Box response was interrupted"), 0)
	}
	return sdkgo.NewMutationBranch(success, DeletedBoxItem{ID: itemID}, nil, sdkgo.Receipt{})
}

func boxMutationFailure(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeletedBoxItem] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, DeletedBoxItem{}, &failure, sdkgo.Receipt{})
}

func validBoxItemID(itemID string, allowRoot bool) bool {
	return itemID != "" && len(itemID) <= 40 && strings.Trim(itemID, "0123456789") == "" && (allowRoot || itemID != "0")
}

func validBoxPage(limit, offset, maximumLimit int) bool {
	return limit >= 0 && limit <= maximumLimit && offset >= 0 && offset <= 10000
}

func boxPageQuery(limit, offset int) string {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		query.Set("offset", strconv.Itoa(offset))
	}
	if len(query) == 0 {
		return ""
	}
	return "?" + query.Encode()
}

func boxSearchPath(queryText, itemType string, limit, offset int) string {
	query := url.Values{"query": {queryText}, "type": {itemType}}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		query.Set("offset", strconv.Itoa(offset))
	}
	return "/2.0/search?" + query.Encode()
}

func decodeBoxItemPage(body []byte) (BoxItemPage, error) {
	var page BoxItemPage
	err := json.Unmarshal(body, &page)
	if err == nil && (page.Entries == nil || page.Limit < 0 || page.Offset < 0 || page.TotalCount < 0) {
		err = fmt.Errorf("Box item page is invalid")
	}
	if err == nil {
		for _, item := range page.Entries {
			if !validBoxItemID(item.ID, true) || item.Name == "" || (item.Type != "file" && item.Type != "folder" && item.Type != "web_link") {
				err = fmt.Errorf("Box item page contains invalid item")
				break
			}
		}
	}
	return page, err
}
