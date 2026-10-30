// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	searchFilesOperationID = "searchFiles"
	// searchItemFields selects the FileSummary fields of every listed or found item.
	searchItemFields  = "id,name,size,webUrl,lastModifiedDateTime,parentReference,file,folder,package"
	maxPageTokenBytes = 8192
	// maxSearchTextBytes bounds the name sent as Graph search text.
	maxSearchTextBytes = 255
)

// NameMatch selects how SearchFiles compares an item name.
type NameMatch string

const (
	// NameMatchExact matches the complete name, ignoring case as OneDrive and
	// SharePoint do. It is the default when Name is set and NameMatch is blank.
	NameMatchExact NameMatch = "exact"
	// NameMatchContains matches names that contain Name, ignoring case.
	NameMatchContains NameMatch = "contains"
)

// SearchFilesInput selects one page of items. With ParentFolderID set, the
// operation reads that folder's direct children, which is strongly consistent;
// with it blank, it runs Microsoft Graph's drive-wide search, which uses an
// index that can lag recent changes and requires Name.
type SearchFilesInput struct {
	// DriveID is the drive to search, such as a drivePicker result; blank means
	// the signed-in user's OneDrive and is invalid for app-only connections.
	DriveID string `json:"driveId,omitempty"`
	// ParentFolderID limits results to direct children of one folder item ID, or
	// root for the drive root; blank searches the whole drive.
	ParentFolderID string `json:"parentFolderId,omitempty"`
	// Name is the item name to match; blank lists every child of ParentFolderID.
	Name string `json:"name,omitempty"`
	// NameMatch selects exact or contains matching; blank means exact.
	NameMatch NameMatch `json:"nameMatch,omitempty"`
	// MimeType restricts results to files of one media type, such as text/plain; blank matches files and folders.
	MimeType string `json:"mimeType,omitempty"`
	// PageSize is the page size from 1 to 200; zero uses the connection's searchPageSize.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken continues a previous search with its NextPageToken; repeat the
	// same filters with it. Blank starts at the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// FileSummary is the partial item record SearchFiles returns for name-to-ID resolution.
type FileSummary struct {
	// ID is the stable drive item ID.
	ID string `json:"id"`
	// Name is the file or folder name.
	Name string `json:"name"`
	// DriveID is the ID of the drive that holds the item.
	DriveID string `json:"driveId,omitempty"`
	// ParentFolderID is the parent folder's item ID when Graph reports it.
	ParentFolderID string `json:"parentFolderId,omitempty"`
	// IsFolder reports a folder.
	IsFolder bool `json:"isFolder"`
	// IsPackage reports a package, such as a OneNote notebook.
	IsPackage bool `json:"isPackage,omitempty"`
	// MimeType is the file's media type; empty for folders.
	MimeType string `json:"mimeType,omitempty"`
	// SizeBytes is the item size reported by Graph.
	SizeBytes int64 `json:"sizeBytes"`
	// LastModifiedDateTime is when the item was last modified.
	LastModifiedDateTime time.Time `json:"lastModifiedDateTime,omitzero"`
	// WebURL opens the item in a browser for a user who can access it.
	WebURL string `json:"webUrl,omitempty"`
}

// SearchFilesOutput is one page of matching items in Graph's order.
type SearchFilesOutput struct {
	// Files lists the page's matches. Filtering happens per page, so a page can
	// be short or empty while NextPageToken is set.
	Files []FileSummary `json:"files"`
	// NextPageToken continues the search; blank means Graph has no further pages.
	NextPageToken string `json:"nextPageToken,omitempty"`
	// IsIndexedSearch reports a drive-wide search, whose index can lag recent
	// changes, so an absent match is not conclusive.
	IsIndexedSearch bool `json:"isIndexedSearch,omitempty"`
}

// SearchFilesOperation implements the searchFiles connector operation.
type SearchFilesOperation struct{ client *Client }

// searchMode is the Graph request one searchFiles input maps to.
type searchMode int

const (
	searchModeChildPath searchMode = iota + 1
	searchModeChildren
	searchModeDriveSearch
)

var searchFilesReadBranches = readBranches{
	operationID: searchFilesOperationID, notFound: SearchFilesBranchProviderRejected,
	providerRejected: SearchFilesBranchProviderRejected, invalidResponse: SearchFilesBranchInvalidResponse,
	defect: SearchFilesBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (SearchFilesOperation) Definition() sdkgo.QueryDefinition { return SearchFilesDefinition }

// Invoke sends one Graph request and classifies its attempt. An exact name in a
// folder is read by path; other folder filters list the folder's children; a
// drive-wide name runs Graph search. Invalid input selects defect without a
// request, and an empty final page selects notFound.
func (operation SearchFilesOperation) Invoke(call sdkgo.Call, input SearchFilesInput) sdkgo.QueryAttempt[SearchFilesOutput] {
	client := operation.client
	mode, pageSize, err := operation.validateSearchFilesInput(input)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchFilesBranchDefect, SearchFilesOutput{}, graphFailurePointer(sdkgo.FailureValidation, searchFilesOperationID, err.Error()), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, searchFilesOperationID, input.DriveID)
	if failure != nil {
		return sdkgo.NewQueryBranch(SearchFilesBranchDefect, SearchFilesOutput{}, failure, sdkgo.Receipt{})
	}
	if mode == searchModeChildPath {
		return operation.findChildByPath(call, &credential, input)
	}
	target := input.PageToken
	if target == "" {
		target = operation.searchFilesFirstPageURL(mode, input, pageSize)
	}
	response, outcome := client.sendRead(call, &credential, searchFilesReadBranches, graphRequest{
		method: http.MethodGet, target: target, responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		return queryAttemptFromReadOutcome[SearchFilesOutput](outcome)
	}
	receipt := client.receipt(call, response.requestID, "")
	output, err := operation.decodeSearchFilesPage(response.body, input, mode)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchFilesBranchInvalidResponse, SearchFilesOutput{}, graphFailurePointer(sdkgo.FailureProtocol, searchFilesOperationID, "provider returned an invalid search response"), receipt)
	}
	if len(output.Files) == 0 && output.NextPageToken == "" {
		return sdkgo.NewQueryBranch(SearchFilesBranchNotFound, output, graphFailurePointer(sdkgo.FailureNotFound, searchFilesOperationID, "no item matched the search"), receipt)
	}
	return sdkgo.NewQueryBranch(SearchFilesBranchFound, output, nil, receipt)
}

// findChildByPath reads the one item a folder can hold under an exact name.
func (operation SearchFilesOperation) findChildByPath(call sdkgo.Call, credential *Credentials, input SearchFilesInput) sdkgo.QueryAttempt[SearchFilesOutput] {
	client := operation.client
	branches := searchFilesReadBranches
	branches.notFound = SearchFilesBranchNotFound
	response, outcome := client.sendRead(call, credential, branches, graphRequest{
		method: http.MethodGet, target: client.childPathURL(input.DriveID, input.ParentFolderID, input.Name) + "?" + buildODataQuery([2]string{"$select", searchItemFields}),
		responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		if outcome.branch == SearchFilesBranchNotFound {
			failure := graphFailure(sdkgo.FailureNotFound, searchFilesOperationID, "no item matched the search")
			return sdkgo.NewQueryBranch(SearchFilesBranchNotFound, SearchFilesOutput{Files: []FileSummary{}}, &failure, outcome.receipt)
		}
		return queryAttemptFromReadOutcome[SearchFilesOutput](outcome)
	}
	receipt := client.receipt(call, response.requestID, "")
	item, err := decodeItemResponse(response.body)
	if err != nil || !strings.EqualFold(item.Name, input.Name) {
		return sdkgo.NewQueryBranch(SearchFilesBranchInvalidResponse, SearchFilesOutput{}, graphFailurePointer(sdkgo.FailureProtocol, searchFilesOperationID, "provider returned an invalid item response"), receipt)
	}
	receipt.ProviderObjectID = item.ID
	output := SearchFilesOutput{Files: []FileSummary{}}
	if matchesMimeType(item, input.MimeType) {
		output.Files = append(output.Files, summarizeItem(item))
	}
	if len(output.Files) == 0 {
		return sdkgo.NewQueryBranch(SearchFilesBranchNotFound, output, graphFailurePointer(sdkgo.FailureNotFound, searchFilesOperationID, "no item matched the search"), receipt)
	}
	return sdkgo.NewQueryBranch(SearchFilesBranchFound, output, nil, receipt)
}

// validateSearchFilesInput returns the Graph request mode and effective page size for valid input.
func (operation SearchFilesOperation) validateSearchFilesInput(input SearchFilesInput) (searchMode, int, error) {
	client := operation.client
	if err := validateDriveAndFolderIDs(input.DriveID, input.ParentFolderID); err != nil {
		return 0, 0, err
	}
	switch input.NameMatch {
	case "", NameMatchExact, NameMatchContains:
	default:
		return 0, 0, errors.New("nameMatch must be exact or contains")
	}
	if input.NameMatch != "" && input.Name == "" {
		return 0, 0, errors.New("nameMatch requires a name")
	}
	if input.Name != "" {
		if err := validateSearchName(input.Name); err != nil {
			return 0, 0, err
		}
	}
	if input.MimeType != "" && !isPlainMediaType(input.MimeType) {
		return 0, 0, errors.New("mimeType must be one lowercase type/subtype without parameters")
	}
	if input.PageToken != "" && (len(input.PageToken) > maxPageTokenBytes || strings.ContainsAny(input.PageToken, " \r\n\t") || !client.isGraphNextLink(input.PageToken)) {
		return 0, 0, errors.New("pageToken must be a Microsoft Graph next-page link from an earlier searchFiles result")
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = client.searchPageSize
	}
	if pageSize < 1 || pageSize > maxSearchPageSize {
		return 0, 0, fmt.Errorf("pageSize must be from 1 to %d", maxSearchPageSize)
	}
	switch {
	case input.ParentFolderID == "" && input.Name == "":
		return 0, 0, errors.New("name is required when parentFolderId is blank, because drive search needs search text")
	case input.ParentFolderID == "":
		return searchModeDriveSearch, pageSize, nil
	case input.Name != "" && input.NameMatch != NameMatchContains && input.PageToken == "":
		return searchModeChildPath, pageSize, nil
	default:
		return searchModeChildren, pageSize, nil
	}
}

// validateSearchName rejects names that no OneDrive or SharePoint item can have.
func validateSearchName(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return errors.New("name cannot be only whitespace")
	case len(name) > maxSearchTextBytes || !utf8.ValidString(name):
		return fmt.Errorf("name must be valid UTF-8 of at most %d bytes", maxSearchTextBytes)
	case strings.ContainsAny(name, reservedItemNameCharacters):
		return errors.New(`name cannot contain / \ * < > ? : | # % or "`)
	}
	for _, character := range name {
		if character < 0x20 || character == 0x7f {
			return errors.New("name cannot contain control characters")
		}
	}
	return nil
}

func (operation SearchFilesOperation) searchFilesFirstPageURL(mode searchMode, input SearchFilesInput, pageSize int) string {
	client := operation.client
	query := "?" + buildODataQuery([2]string{"$select", searchItemFields}, [2]string{"$top", strconv.Itoa(pageSize)})
	if mode == searchModeChildren {
		return client.itemURL(input.DriveID, input.ParentFolderID) + "/children" + query
	}
	// OData doubles a single quote inside a string literal.
	searchText := url.PathEscape(strings.ReplaceAll(input.Name, "'", "''"))
	return client.driveURL(input.DriveID) + "/root/search(q='" + searchText + "')" + query
}

// decodeSearchFilesPage validates one Graph page and applies the name and media-type filters.
func (operation SearchFilesOperation) decodeSearchFilesPage(content []byte, input SearchFilesInput, mode searchMode) (SearchFilesOutput, error) {
	client := operation.client
	var response graphCollectionResponse
	if err := json.Unmarshal(content, &response); err != nil {
		return SearchFilesOutput{}, err
	}
	if len(response.Value) > maxSearchPageSize {
		return SearchFilesOutput{}, errors.New("search response exceeds the page size limit")
	}
	if response.NextLink != "" && !client.isGraphNextLink(response.NextLink) {
		return SearchFilesOutput{}, errors.New("search response next link is not a Microsoft Graph URL")
	}
	output := SearchFilesOutput{Files: make([]FileSummary, 0, len(response.Value)), NextPageToken: response.NextLink, IsIndexedSearch: mode == searchModeDriveSearch}
	if len(output.NextPageToken) > maxPageTokenBytes {
		return SearchFilesOutput{}, errors.New("search response next link is too long")
	}
	for _, resource := range response.Value {
		item, err := convertItemResource(resource)
		if err != nil {
			return SearchFilesOutput{}, err
		}
		if matchesName(item.Name, input) && matchesMimeType(item, input.MimeType) {
			output.Files = append(output.Files, summarizeItem(item))
		}
	}
	return output, nil
}

func matchesName(name string, input SearchFilesInput) bool {
	switch {
	case input.Name == "":
		return true
	case input.NameMatch == NameMatchContains:
		return strings.Contains(strings.ToLower(name), strings.ToLower(input.Name))
	default:
		return strings.EqualFold(name, input.Name)
	}
}

func matchesMimeType(item DriveItem, mimeType string) bool {
	return mimeType == "" || (!item.IsFolder && strings.EqualFold(item.MimeType, mimeType))
}

func summarizeItem(item DriveItem) FileSummary {
	return FileSummary{
		ID: item.ID, Name: item.Name, DriveID: item.DriveID, ParentFolderID: item.ParentFolderID,
		IsFolder: item.IsFolder, IsPackage: item.IsPackage, MimeType: item.MimeType, SizeBytes: item.SizeBytes,
		LastModifiedDateTime: item.LastModifiedDateTime, WebURL: item.WebURL,
	}
}
