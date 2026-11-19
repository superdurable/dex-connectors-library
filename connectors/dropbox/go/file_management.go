// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dropbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// SearchFilesInput selects one bounded search page by name.
type SearchFilesInput struct {
	// Query is a nonempty name fragment, up to 1000 characters.
	Query string `json:"query"`
	// Path limits the search to one folder; blank searches the connected root.
	Path string `json:"path,omitempty"`
	// MaxResults is the requested page size, from 1 to 1000; zero selects 20.
	MaxResults int `json:"maxResults,omitempty"`
}

// SearchFilesPage contains one page of matching file and folder identities.
type SearchFilesPage struct {
	// Entries contains the file and folder matches.
	Entries []EntryMetadata `json:"entries"`
	// Cursor continues the search when HasMore is true.
	Cursor string `json:"cursor,omitempty"`
	// HasMore reports whether another page is available.
	HasMore bool `json:"hasMore"`
}

// EnsureFolderInput selects an absolute folder path to create if absent.
type EnsureFolderInput struct {
	// Path is the absolute Dropbox folder path.
	Path string `json:"path"`
}

// EnsuredFolder confirms that the selected folder exists.
type EnsuredFolder struct {
	// Path is the absolute folder path.
	Path string `json:"path"`
}

// DeletePathInput selects a file or folder to delete if present.
type DeletePathInput struct {
	// Path is an absolute Dropbox path or stable file or folder ID.
	Path string `json:"path"`
}

// DeletedPath confirms that the selected path is absent.
type DeletedPath struct {
	// Path is the requested file or folder path or ID.
	Path string `json:"path"`
}

// SearchFilesOperation searches Dropbox file and folder names.
type SearchFilesOperation struct{ client *Client }

// EnsureFolderOperation creates a folder if needed.
type EnsureFolderOperation struct{ client *Client }

// DeletePathOperation deletes a file or folder if present.
type DeletePathOperation struct{ client *Client }

// SearchFiles binds name search to this client.
func (client *Client) SearchFiles() SearchFilesOperation { return SearchFilesOperation{client} }

// EnsureFolder binds folder creation to this client.
func (client *Client) EnsureFolder() EnsureFolderOperation { return EnsureFolderOperation{client} }

// DeletePath binds file or folder deletion to this client.
func (client *Client) DeletePath() DeletePathOperation { return DeletePathOperation{client} }

// Definition returns the generated search Query contract.
func (SearchFilesOperation) Definition() sdkgo.QueryDefinition { return SearchFilesDefinition }

// Definition returns the generated folder Mutation contract.
func (EnsureFolderOperation) Definition() sdkgo.MutationDefinition { return EnsureFolderDefinition }

// Definition returns the generated delete Mutation contract.
func (DeletePathOperation) Definition() sdkgo.MutationDefinition { return DeletePathDefinition }

// IdempotencyKey derives a stable retry identity for folder creation.
func (EnsureFolderOperation) IdempotencyKey(callID sdkgo.CallID, _ EnsureFolderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable retry identity for deletion.
func (DeletePathOperation) IdempotencyKey(callID sdkgo.CallID, _ DeletePathInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke searches one page without including highlights or content.
func (operation SearchFilesOperation) Invoke(call sdkgo.Call, input SearchFilesInput) sdkgo.QueryAttempt[SearchFilesPage] {
	branches := queryBranches{found: SearchFilesBranchFound, rejected: SearchFilesBranchProviderRejected, invalid: SearchFilesBranchInvalidResponse, defect: SearchFilesBranchDefect}
	if input.Query == "" || len(input.Query) > 1000 || !utf8.ValidString(input.Query) || strings.TrimSpace(input.Query) != input.Query || !validDropboxPath(input.Path, true) || input.MaxResults < 0 || input.MaxResults > 1000 {
		return queryBranch("searchFiles", branches.defect, SearchFilesPage{}, sdkgo.FailureValidation, "search query, path, or page size is invalid")
	}
	if input.MaxResults == 0 {
		input.MaxResults = 20
	}
	payload, err := json.Marshal(struct {
		Query   string `json:"query"`
		Options struct {
			MaxResults int    `json:"max_results"`
			Path       string `json:"path,omitempty"`
		} `json:"options"`
	}{Query: input.Query, Options: struct {
		MaxResults int    `json:"max_results"`
		Path       string `json:"path,omitempty"`
	}{MaxResults: input.MaxResults, Path: input.Path}})
	if err != nil {
		return queryBranch("searchFiles", branches.defect, SearchFilesPage{}, sdkgo.FailureLocalDefect, "search request could not be built")
	}
	return invokeQuery(call, operation.client, "searchFiles", "/2/files/search_v2", payload, branches, decodeSearchFilesPage)
}

// Invoke creates the folder or accepts a provider-confirmed existing folder.
func (operation EnsureFolderOperation) Invoke(call sdkgo.Call, input EnsureFolderInput) sdkgo.MutationAttempt[EnsuredFolder] {
	if !validAbsoluteDropboxPath(input.Path) {
		return fileMutationBranch("ensureFolder", EnsureFolderBranchDefect, EnsuredFolder{}, sdkgo.FailureValidation, "folder path must be absolute")
	}
	payload, err := json.Marshal(struct {
		Path       string `json:"path"`
		Autorename bool   `json:"autorename"`
	}{Path: input.Path})
	if err != nil {
		return fileMutationBranch("ensureFolder", EnsureFolderBranchDefect, EnsuredFolder{}, sdkgo.FailureLocalDefect, "folder request could not be built")
	}
	return invokeFileMutation(call, operation.client, "ensureFolder", "/2/files/create_folder_v2", payload, EnsureFolderBranchEnsured, EnsureFolderBranchProviderRejected, EnsureFolderBranchInvalidResponse, EnsureFolderBranchDefect, EnsuredFolder{Path: input.Path}, "path/conflict/folder", func(body []byte) (EnsuredFolder, error) {
		var wire struct {
			Metadata json.RawMessage `json:"metadata"`
		}
		if err := json.Unmarshal(body, &wire); err != nil {
			return EnsuredFolder{}, err
		}
		entry, err := decodeEntry(wire.Metadata)
		if err != nil || entry.Kind != "folder" || entry.PathDisplay == "" {
			return EnsuredFolder{}, fmt.Errorf("folder metadata is invalid")
		}
		return EnsuredFolder{Path: input.Path}, nil
	})
}

// Invoke deletes one file or folder, accepting a provider-confirmed missing path.
func (operation DeletePathOperation) Invoke(call sdkgo.Call, input DeletePathInput) sdkgo.MutationAttempt[DeletedPath] {
	if !validDropboxPath(input.Path, false) || strings.HasPrefix(input.Path, "rev:") {
		return fileMutationBranch("deletePath", DeletePathBranchDefect, DeletedPath{}, sdkgo.FailureValidation, "delete path is invalid")
	}
	payload, err := json.Marshal(struct {
		Path string `json:"path"`
	}{Path: input.Path})
	if err != nil {
		return fileMutationBranch("deletePath", DeletePathBranchDefect, DeletedPath{}, sdkgo.FailureLocalDefect, "delete request could not be built")
	}
	return invokeFileMutation(call, operation.client, "deletePath", "/2/files/delete_v2", payload, DeletePathBranchDeleted, DeletePathBranchProviderRejected, DeletePathBranchInvalidResponse, DeletePathBranchDefect, DeletedPath{Path: input.Path}, "path_lookup/not_found", func(body []byte) (DeletedPath, error) {
		var wire struct {
			Metadata json.RawMessage `json:"metadata"`
		}
		if err := json.Unmarshal(body, &wire); err != nil {
			return DeletedPath{}, err
		}
		if _, err := decodeEntry(wire.Metadata); err != nil {
			return DeletedPath{}, err
		}
		return DeletedPath{Path: input.Path}, nil
	})
}

func decodeSearchFilesPage(body []byte) (SearchFilesPage, error) {
	var wire struct {
		Matches []struct {
			Metadata struct {
				Metadata json.RawMessage `json:"metadata"`
			} `json:"metadata"`
		} `json:"matches"`
		Cursor  string `json:"cursor"`
		HasMore *bool  `json:"has_more"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return SearchFilesPage{}, err
	}
	if wire.Matches == nil || wire.HasMore == nil || (*wire.HasMore && wire.Cursor == "") {
		return SearchFilesPage{}, fmt.Errorf("search page is incomplete")
	}
	page := SearchFilesPage{Entries: make([]EntryMetadata, 0, len(wire.Matches)), Cursor: wire.Cursor, HasMore: *wire.HasMore}
	for _, match := range wire.Matches {
		entry, err := decodeEntry(match.Metadata.Metadata)
		if err != nil {
			return SearchFilesPage{}, err
		}
		page.Entries = append(page.Entries, entry)
	}
	return page, nil
}

func validAbsoluteDropboxPath(path string) bool {
	return strings.HasPrefix(path, "/") && path != "/" && !strings.HasSuffix(path, "/") && validDropboxPath(path, false)
}

func invokeFileMutation[T any](call sdkgo.Call, client *Client, operation, endpoint string, payload []byte, success, rejected, invalid, defect sdkgo.BranchID, conflictValue T, acceptedConflict string, decode func([]byte) (T, error)) sdkgo.MutationAttempt[T] {
	var zero T
	body, status, retryAfter, err := client.post(call, endpoint, payload)
	if errors.Is(err, errCredential) {
		return fileMutationBranch(operation, defect, zero, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(err, errRefresh) {
		return sdkgo.NewMutationRetry[T](queryFailure(operation, sdkgo.FailureAvailability, "Dropbox credentials could not be refreshed"), 0)
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[T](queryFailure(operation, sdkgo.FailureTransport, "Dropbox request failed"), 0)
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[T](queryFailure(operation, sdkgo.FailureRateLimit, "Dropbox rate limited the request"), retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[T](queryFailure(operation, sdkgo.FailureAvailability, "Dropbox is temporarily unavailable"), retryAfter)
	}
	if status == http.StatusConflict {
		var providerError struct {
			ErrorSummary string `json:"error_summary"`
		}
		if json.Unmarshal(body, &providerError) == nil && strings.HasPrefix(providerError.ErrorSummary, acceptedConflict) {
			return sdkgo.NewMutationBranch(success, conflictValue, nil, sdkgo.Receipt{})
		}
	}
	if status != http.StatusOK {
		return fileMutationBranch(operation, rejected, zero, sdkgo.FailureProviderRejection, "Dropbox rejected HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return fileMutationBranch(operation, invalid, zero, sdkgo.FailureResponseTooLarge, "Dropbox response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[T](queryFailure(operation, sdkgo.FailureTransport, "Dropbox response was interrupted"), 0)
	}
	value, err := decode(body)
	if err != nil {
		return fileMutationBranch(operation, invalid, zero, sdkgo.FailureProtocol, "Dropbox returned invalid JSON")
	}
	return sdkgo.NewMutationBranch(success, value, nil, sdkgo.Receipt{})
}

func fileMutationBranch[T any](operation string, branch sdkgo.BranchID, value T, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}
