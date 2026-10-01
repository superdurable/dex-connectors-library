// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drive

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	searchFilesOperationID = "searchFiles"
	searchFileFields       = "nextPageToken,incompleteSearch,files(id,name,mimeType,parents,modifiedTime,webViewLink)"
	// searchFilesOrder lists newest files first, the order Drive recommends for large collections.
	searchFilesOrder  = "modifiedTime desc,name"
	maxPageTokenBytes = 4096
)

// NameMatch selects how SearchFiles compares a file name.
type NameMatch string

const (
	// NameMatchExact matches the complete file name with Drive's name = operator.
	// It is the default when Name is set and NameMatch is blank.
	NameMatchExact NameMatch = "exact"
	// NameMatchContains uses Drive's name contains operator, which Google
	// documents as prefix matching of name terms: "Hello" finds "HelloWorld"
	// but "World" does not.
	NameMatchContains NameMatch = "contains"
)

// SearchFilesInput filters one page of non-trashed Drive files. Every set
// filter must match; an input without filters lists recent files.
type SearchFilesInput struct {
	// Name is the file name to match; blank matches any name.
	Name string `json:"name,omitempty"`
	// NameMatch selects exact or prefix-term matching; blank means exact.
	NameMatch NameMatch `json:"nameMatch,omitempty"`
	// MimeType restricts results to one Drive MIME type, such as
	// application/vnd.google-apps.spreadsheet; blank matches any type.
	MimeType string `json:"mimeType,omitempty"`
	// ParentFolderID restricts results to direct children of one folder ID, or
	// root for the My Drive root; blank searches every visible folder.
	ParentFolderID string `json:"parentFolderId,omitempty"`
	// PageSize is the page size from 1 to 100; zero uses the connection's searchPageSize.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken continues a previous search with its NextPageToken; blank starts at the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// SearchFilesOutput is one page of matching files, newest modification first.
type SearchFilesOutput struct {
	// Files lists the page's matches. Drive may return a short or empty page
	// while NextPageToken is set.
	Files []FileSummary `json:"files"`
	// NextPageToken continues the search; blank means Drive has no further pages.
	NextPageToken string `json:"nextPageToken,omitempty"`
	// IsIncompleteSearch reports that Drive did not search every document, so
	// an absent match is not conclusive.
	IsIncompleteSearch bool `json:"isIncompleteSearch,omitempty"`
}

// SearchFilesOperation implements the searchFiles connector operation.
type SearchFilesOperation struct{ client *Client }

type searchFilesResponse struct {
	Files            []driveFileResource `json:"files"`
	NextPageToken    string              `json:"nextPageToken"`
	IncompleteSearch bool                `json:"incompleteSearch"`
}

var searchFilesReadBranches = readBranches{
	operationID: searchFilesOperationID, notFound: SearchFilesBranchProviderRejected,
	tooLarge: SearchFilesBranchInvalidResponse, providerRejected: SearchFilesBranchProviderRejected,
	invalidResponse: SearchFilesBranchInvalidResponse, defect: SearchFilesBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (SearchFilesOperation) Definition() sdkgo.QueryDefinition { return SearchFilesDefinition }

// Invoke sends one Drive files.list request and classifies its attempt.
// Invalid input selects defect without a provider request; an empty final page selects notFound.
func (operation SearchFilesOperation) Invoke(call sdkgo.Call, input SearchFilesInput) sdkgo.QueryAttempt[SearchFilesOutput] {
	client := operation.client
	pageSize, err := validateSearchFilesInput(input, client.searchPageSize)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchFilesBranchDefect, SearchFilesOutput{}, driveFailurePointer(sdkgo.FailureValidation, searchFilesOperationID, err.Error()), sdkgo.Receipt{})
	}
	credential, failure := client.resolveCredential(call, searchFilesOperationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(SearchFilesBranchDefect, SearchFilesOutput{}, failure, sdkgo.Receipt{})
	}
	query := url.Values{
		"q": {buildSearchQuery(input)}, "pageSize": {strconv.Itoa(pageSize)}, "orderBy": {searchFilesOrder},
		"fields": {searchFileFields}, "spaces": {"drive"},
	}
	if input.PageToken != "" {
		query.Set("pageToken", input.PageToken)
	}
	response, outcome := client.sendRead(call, &credential, searchFilesReadBranches, driveRequest{
		method: http.MethodGet, target: client.filesListURL(query), responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		return queryAttemptFromReadOutcome[SearchFilesOutput](outcome)
	}
	receipt := client.receipt(call, response.requestID, "")
	output, err := decodeSearchFilesResponse(response.body, pageSize)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchFilesBranchInvalidResponse, SearchFilesOutput{}, driveFailurePointer(sdkgo.FailureProtocol, searchFilesOperationID, "provider returned an invalid search response"), receipt)
	}
	if len(output.Files) == 0 && output.NextPageToken == "" {
		return sdkgo.NewQueryBranch(SearchFilesBranchNotFound, output, driveFailurePointer(sdkgo.FailureNotFound, searchFilesOperationID, "no file matched the search"), receipt)
	}
	return sdkgo.NewQueryBranch(SearchFilesBranchFound, output, nil, receipt)
}

// validateSearchFilesInput returns the effective page size for valid input.
func validateSearchFilesInput(input SearchFilesInput, defaultPageSize int) (int, error) {
	if input.Name != "" && strings.TrimSpace(input.Name) == "" {
		return 0, errors.New("name cannot be only whitespace")
	}
	switch input.NameMatch {
	case "", NameMatchExact, NameMatchContains:
	default:
		return 0, errors.New("nameMatch must be exact or contains")
	}
	if input.NameMatch != "" && input.Name == "" {
		return 0, errors.New("nameMatch requires a name")
	}
	if input.MimeType != "" && !isPlainMediaType(input.MimeType) {
		return 0, errors.New("mimeType must be one lowercase type/subtype without parameters")
	}
	if input.ParentFolderID != "" && !isDriveID(input.ParentFolderID) {
		return 0, errors.New("parentFolderId must be a Drive folder ID or root")
	}
	if len(input.PageToken) > maxPageTokenBytes || strings.ContainsAny(input.PageToken, " \r\n\t") {
		return 0, errors.New("pageToken is not a Drive page token")
	}
	if input.PageSize == 0 {
		return defaultPageSize, nil
	}
	if input.PageSize < 1 || input.PageSize > maxSearchPageSize {
		return 0, fmt.Errorf("pageSize must be from 1 to %d", maxSearchPageSize)
	}
	return input.PageSize, nil
}

// buildSearchQuery builds a Drive q expression that always excludes trashed files.
func buildSearchQuery(input SearchFilesInput) string {
	var terms []string
	if input.Name != "" {
		operator := "="
		if input.NameMatch == NameMatchContains {
			operator = "contains"
		}
		terms = append(terms, "name "+operator+" "+quoteDriveQueryString(input.Name))
	}
	if input.MimeType != "" {
		terms = append(terms, "mimeType = "+quoteDriveQueryString(input.MimeType))
	}
	if input.ParentFolderID != "" {
		terms = append(terms, quoteDriveQueryString(input.ParentFolderID)+" in parents")
	}
	terms = append(terms, "trashed = false")
	return strings.Join(terms, " and ")
}

func decodeSearchFilesResponse(content []byte, pageSize int) (SearchFilesOutput, error) {
	var response searchFilesResponse
	if err := json.Unmarshal(content, &response); err != nil {
		return SearchFilesOutput{}, err
	}
	if len(response.Files) > pageSize {
		return SearchFilesOutput{}, errors.New("search response exceeds the requested page size")
	}
	output := SearchFilesOutput{
		Files: make([]FileSummary, 0, len(response.Files)), NextPageToken: response.NextPageToken,
		IsIncompleteSearch: response.IncompleteSearch,
	}
	for _, resource := range response.Files {
		if !isDriveID(resource.ID) || resource.Name == "" || resource.MimeType == "" {
			return SearchFilesOutput{}, errors.New("search response contains an invalid file")
		}
		output.Files = append(output.Files, FileSummary{
			ID: resource.ID, Name: resource.Name, MimeType: resource.MimeType, Parents: resource.Parents,
			ModifiedTime: resource.ModifiedTime, WebViewLink: resource.WebViewLink,
		})
	}
	return output, nil
}
