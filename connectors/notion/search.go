// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	searchOperationID     = "search"
	searchFailureSubject  = "search"
	defaultSearchPageSize = 20
	// MaximumPageSize is Notion's largest page of search, query, and block results.
	MaximumPageSize         = 100
	maximumSearchQueryRunes = 1000
)

// SearchObjectType limits a search to pages or data sources.
type SearchObjectType string

const (
	// SearchObjectPage matches pages, including database rows.
	SearchObjectPage SearchObjectType = "page"
	// SearchObjectDataSource matches data sources, the tables inside databases.
	SearchObjectDataSource SearchObjectType = "data_source"
)

// SearchInput selects one page of title matches among the pages and data
// sources shared with the connection.
type SearchInput struct {
	// Query is matched against titles; blank lists every shared page and data source.
	Query string `json:"query,omitempty"`
	// ObjectType limits the results to pages or data sources; blank returns both.
	ObjectType SearchObjectType `json:"objectType,omitempty"`
	// SortDirection orders by last edit time; blank keeps Notion's relevance order.
	SortDirection SortDirection `json:"sortDirection,omitempty"`
	// PageSize is the number of results to request, 1 to 100; zero requests 20.
	PageSize int `json:"pageSize,omitempty"`
	// StartCursor continues a previous search with the same input; blank reads the first page.
	StartCursor string `json:"startCursor,omitempty"`
}

// SearchOutput is one page of title matches.
type SearchOutput struct {
	// Matches lists the page's results in Notion's order.
	Matches []SearchMatch `json:"matches"`
	// NextCursor continues the search; it is empty on the last page.
	NextCursor string `json:"nextCursor,omitempty"`
	// HasMore reports that another page of results exists.
	HasMore bool `json:"hasMore"`
}

// SearchMatch is one page or data source whose title matched.
type SearchMatch struct {
	// ObjectType is page or data_source.
	ObjectType SearchObjectType `json:"objectType"`
	// ID is the page or data source ID.
	ID string `json:"id"`
	// Title is the plain-text title.
	Title string `json:"title"`
	// URL opens the object in Notion; it is not a stable identifier.
	URL string `json:"url,omitempty"`
	// Parent contains the object. A data source's parent is its database.
	Parent Parent `json:"parent"`
	// LastEditedTime is the RFC 3339 time of the last edit.
	LastEditedTime string `json:"lastEditedTime,omitempty"`
	// IsInTrash reports that the object is in the trash.
	IsInTrash bool `json:"inTrash"`
}

// SearchOperation implements the search Query.
type SearchOperation struct{ client *Client }

type searchRequestBody struct {
	Query       string            `json:"query,omitempty"`
	Filter      map[string]string `json:"filter,omitempty"`
	Sort        map[string]string `json:"sort,omitempty"`
	StartCursor string            `json:"start_cursor,omitempty"`
	PageSize    int               `json:"page_size"`
}

type searchResponseBody struct {
	Results    []searchResultResource `json:"results"`
	NextCursor *string                `json:"next_cursor"`
	HasMore    bool                   `json:"has_more"`
}

type searchResultResource struct {
	Object         string             `json:"object"`
	ID             string             `json:"id"`
	URL            string             `json:"url"`
	LastEditedTime string             `json:"last_edited_time"`
	InTrash        bool               `json:"in_trash"`
	Parent         parentResource     `json:"parent"`
	Title          []richTextResource `json:"title"`
	Properties     json.RawMessage    `json:"properties"`
}

// Definition returns the immutable connector operation definition.
func (SearchOperation) Definition() sdkgo.QueryDefinition { return SearchDefinition }

// Invoke reads one search page. Transport failures, 408, 409, 429, 529, and 5xx are retried.
func (operation SearchOperation) Invoke(call sdkgo.Call, input SearchInput) sdkgo.QueryAttempt[SearchOutput] {
	client := operation.client
	body, err := buildSearchRequestBody(input)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchBranchDefect, SearchOutput{}, validationFailure(searchOperationID, err), sdkgo.Receipt{})
	}
	session, cancel, sessionFailure := client.startSession(call, searchOperationID, readOperationDeadline)
	if sessionFailure != nil {
		return sdkgo.NewQueryBranch(SearchBranchDefect, SearchOutput{}, sessionFailure, sdkgo.Receipt{})
	}
	defer cancel()
	result := client.exchange(session, notionRequest{method: http.MethodPost, path: "/search", payload: body})
	receipt := client.receipt(session, result.response, "")
	classification := client.classifyRead(searchOperationID, searchFailureSubject, result)
	switch classification.outcome {
	case readSucceeded:
	case readRetry:
		return sdkgo.NewQueryRetry[SearchOutput](classification.failure, classification.retryAfter)
	case readDefect:
		return sdkgo.NewQueryBranch(SearchBranchDefect, SearchOutput{}, &classification.failure, receipt)
	case readInvalid:
		return sdkgo.NewQueryBranch(SearchBranchInvalidResponse, SearchOutput{}, &classification.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(SearchBranchProviderRejected, SearchOutput{}, &classification.failure, receipt)
	}
	output, err := decodeSearchPage(result.response.body, body.PageSize)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchBranchInvalidResponse, SearchOutput{}, failurePointer(searchOperationID, sdkgo.FailureProtocol, err.Error()), receipt)
	}
	if len(output.Matches) == 0 && body.StartCursor == "" {
		return sdkgo.NewQueryBranch(SearchBranchNoMatch, output,
			failurePointer(searchOperationID, sdkgo.FailureNotFound, "no page or data source shared with the connection has a title containing the query"), receipt)
	}
	return sdkgo.NewQueryBranch(SearchBranchSearched, output, nil, receipt)
}

func buildSearchRequestBody(input SearchInput) (searchRequestBody, error) {
	body := searchRequestBody{Query: strings.TrimSpace(input.Query), PageSize: input.PageSize}
	if utf8.RuneCountInString(body.Query) > maximumSearchQueryRunes {
		return searchRequestBody{}, fmt.Errorf("query is at most %d characters", maximumSearchQueryRunes)
	}
	switch input.ObjectType {
	case "":
	case SearchObjectPage, SearchObjectDataSource:
		body.Filter = map[string]string{"property": "object", "value": string(input.ObjectType)}
	default:
		return searchRequestBody{}, errors.New("objectType is page, data_source, or blank")
	}
	switch input.SortDirection {
	case "":
	case SortAscending, SortDescending:
		body.Sort = map[string]string{"timestamp": "last_edited_time", "direction": string(input.SortDirection)}
	default:
		return searchRequestBody{}, errors.New("sortDirection is ascending, descending, or blank")
	}
	switch {
	case body.PageSize == 0:
		body.PageSize = defaultSearchPageSize
	case body.PageSize < 1 || body.PageSize > MaximumPageSize:
		return searchRequestBody{}, fmt.Errorf("pageSize must be between 1 and %d", MaximumPageSize)
	}
	cursor, err := validateCursor(input.StartCursor)
	if err != nil {
		return searchRequestBody{}, err
	}
	body.StartCursor = cursor
	return body, nil
}

func decodeSearchPage(contents []byte, pageSize int) (SearchOutput, error) {
	var page searchResponseBody
	if err := json.Unmarshal(contents, &page); err != nil || len(page.Results) > pageSize {
		return SearchOutput{}, errors.New("Notion returned an invalid search page")
	}
	output := SearchOutput{Matches: make([]SearchMatch, 0, len(page.Results)), HasMore: page.HasMore}
	nextCursor, err := validateCursor(dereferenceText(page.NextCursor))
	if err != nil {
		return SearchOutput{}, errors.New("Notion returned an invalid search cursor")
	}
	if page.HasMore {
		output.NextCursor = nextCursor
	}
	for _, resource := range page.Results {
		match, isKnown, err := decodeSearchMatch(resource)
		if err != nil {
			return SearchOutput{}, err
		}
		if isKnown {
			output.Matches = append(output.Matches, match)
		}
	}
	return output, nil
}

// decodeSearchMatch converts a page or data source result; another object type Notion adds is skipped.
func decodeSearchMatch(resource searchResultResource) (SearchMatch, bool, error) {
	match := SearchMatch{
		ObjectType: SearchObjectType(resource.Object), URL: resource.URL, Parent: decodeParent(resource.Parent),
		LastEditedTime: resource.LastEditedTime, IsInTrash: resource.InTrash,
	}
	switch match.ObjectType {
	case SearchObjectPage:
		properties, err := decodeRawProperties(resource.Properties)
		if err != nil {
			return SearchMatch{}, false, errors.New("Notion returned an invalid page in the search results")
		}
		match.Title = titleOfProperties(properties)
	case SearchObjectDataSource:
		match.Title = joinPlainText(resource.Title)
	default:
		return SearchMatch{}, false, nil
	}
	id, err := ParseID(resource.ID)
	if err != nil {
		return SearchMatch{}, false, errors.New("Notion returned a search result without a valid ID")
	}
	match.ID = id
	return match, true, nil
}

// validateCursor accepts blank or one opaque printable cursor; Notion documents cursors as opaque.
func validateCursor(value string) (string, error) {
	cursor := strings.TrimSpace(value)
	if cursor != "" && (len(cursor) > maximumCursorBytes || !isPrintableCursor(cursor)) {
		return "", errors.New("a cursor must be the printable value from a previous page")
	}
	return cursor, nil
}

func isPrintableCursor(cursor string) bool {
	for index := 0; index < len(cursor); index++ {
		if cursor[index] <= ' ' || cursor[index] > '~' {
			return false
		}
	}
	return true
}
