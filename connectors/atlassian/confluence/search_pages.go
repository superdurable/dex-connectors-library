// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	searchPagesOperationID    = "searchPages"
	searchPagesFailureSubject = "page search"
	contentSearchPath         = "/content/search"
	defaultSearchPageSize     = 25
	maximumSearchPageSize     = 100
	maximumCursorBytes        = 4096
)

// SearchPagesInput selects one page of search results.
type SearchPagesInput struct {
	// Filter is the typed search the connector escapes into CQL.
	Filter PageSearchFilter `json:"filter"`
	// PageSize is the number of pages to request, 1 to 100; zero requests 25. Confluence may return fewer.
	PageSize int `json:"pageSize,omitempty"`
	// Cursor continues a previous search with the same filter; blank reads the first page.
	Cursor string `json:"cursor,omitempty"`
}

// SearchPagesOutput is one page of matching pages.
type SearchPagesOutput struct {
	// CQL is the exact query sent to Confluence.
	CQL string `json:"cql"`
	// Pages lists the matching pages in the requested order, without their bodies.
	Pages []PageSummary `json:"pages"`
	// NextCursor continues the search; it is empty on the last page.
	NextCursor string `json:"nextCursor,omitempty"`
}

// PageSummary identifies one page found by search. Read its body with getPage.
type PageSummary struct {
	// ID is the numeric page ID.
	ID string `json:"id"`
	// Title is the page title.
	Title string `json:"title"`
	// Status is Confluence's page status, such as current.
	Status string `json:"status,omitempty"`
	// SpaceID is the numeric ID of the space that holds the page.
	SpaceID string `json:"spaceId,omitempty"`
	// SpaceKey is the key of the space that holds the page, such as OPS.
	SpaceKey string `json:"spaceKey,omitempty"`
	// VersionNumber is the page's current version number.
	VersionNumber int `json:"versionNumber,omitempty"`
	// LastModifiedAt is when the current version was saved, or zero when Confluence omitted it.
	LastModifiedAt time.Time `json:"lastModifiedAt"`
	// WebURL is the page's address in the Confluence web UI, or empty when Confluence omitted it.
	WebURL string `json:"webUrl,omitempty"`
}

// SearchPagesOperation implements the searchPages Query with Confluence's CQL content search.
type SearchPagesOperation struct{ client *Client }

type contentSearchResource struct {
	Results []searchContentResource `json:"results"`
	Links   struct {
		Base string `json:"base"`
		Next string `json:"next"`
	} `json:"_links"`
}

type searchContentResource struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Title  string `json:"title"`
	Space  *struct {
		ID  json.Number `json:"id"`
		Key string      `json:"key"`
	} `json:"space"`
	Version *struct {
		Number int    `json:"number"`
		When   string `json:"when"`
	} `json:"version"`
	Links struct {
		WebUI string `json:"webui"`
	} `json:"_links"`
}

// Definition returns the immutable connector operation definition.
func (SearchPagesOperation) Definition() sdkgo.QueryDefinition { return SearchPagesDefinition }

// Invoke reads one search page. Transport failures, 408, 429, and 5xx responses are retried.
func (operation SearchPagesOperation) Invoke(call sdkgo.Call, input SearchPagesInput) sdkgo.QueryAttempt[SearchPagesOutput] {
	client := operation.client
	query, cql, err := buildSearchQuery(input)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchPagesBranchDefect, SearchPagesOutput{}, failurePointer(searchPagesOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, sessionErr := client.startSession(call, searchPagesOperationID)
	if sessionErr != nil {
		if sessionErr.isRetryable {
			return sdkgo.NewQueryRetry[SearchPagesOutput](sessionErr.failure, sessionErr.retryAfter)
		}
		return sdkgo.NewQueryBranch(SearchPagesBranchDefect, SearchPagesOutput{CQL: cql}, sessionErr.pointer(), sdkgo.Receipt{})
	}
	defer cancel()
	result := client.exchange(session, confluenceRequest{method: http.MethodGet, api: searchAPI, path: contentSearchPath, query: query})
	output := SearchPagesOutput{CQL: cql, Pages: []PageSummary{}}
	classification := client.classifyRead(searchPagesOperationID, searchPagesFailureSubject, result)
	receipt := client.receipt(session, result.response, "")
	switch classification.outcome {
	case readSucceeded:
	case readRetry:
		return sdkgo.NewQueryRetry[SearchPagesOutput](classification.failure, classification.retryAfter)
	case readDefect:
		return sdkgo.NewQueryBranch(SearchPagesBranchDefect, output, &classification.failure, receipt)
	case readInvalid:
		return sdkgo.NewQueryBranch(SearchPagesBranchInvalidResponse, output, &classification.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(SearchPagesBranchProviderRejected, output, &classification.failure, receipt)
	}
	var page contentSearchResource
	if err := json.Unmarshal(result.response.body, &page); err != nil {
		return sdkgo.NewQueryBranch(SearchPagesBranchInvalidResponse, output, failurePointer(searchPagesOperationID, sdkgo.FailureProtocol, "Confluence returned an invalid search page"), receipt)
	}
	nextCursor, err := readNextCursor(page.Links.Next)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchPagesBranchInvalidResponse, output, failurePointer(searchPagesOperationID, sdkgo.FailureProtocol, "Confluence returned an invalid search cursor"), receipt)
	}
	for _, resource := range page.Results {
		summary, isPage, err := decodePageSummary(resource, page.Links.Base)
		if err != nil {
			return sdkgo.NewQueryBranch(SearchPagesBranchInvalidResponse, SearchPagesOutput{CQL: cql, Pages: []PageSummary{}},
				failurePointer(searchPagesOperationID, sdkgo.FailureProtocol, "Confluence returned an invalid page in the search results: "+err.Error()), receipt)
		}
		if !isPage || isModifiedBefore(summary, input.Filter.ModifiedSince) {
			continue
		}
		output.Pages = append(output.Pages, summary)
	}
	output.NextCursor = nextCursor
	return sdkgo.NewQueryBranch(SearchPagesBranchSearched, output, nil, receipt)
}

func buildSearchQuery(input SearchPagesInput) (url.Values, string, error) {
	cql, err := input.Filter.CQL()
	if err != nil {
		return nil, "", err
	}
	pageSize := input.PageSize
	switch {
	case pageSize == 0:
		pageSize = defaultSearchPageSize
	case pageSize < 1 || pageSize > maximumSearchPageSize:
		return nil, "", errors.New("pageSize must be between 1 and 100")
	}
	query := url.Values{"cql": {cql}, "limit": {strconv.Itoa(pageSize)}, "expand": {"space,version"}}
	if cursor := strings.TrimSpace(input.Cursor); cursor != "" {
		if !isCursor(cursor) {
			return nil, "", errors.New("cursor must be the printable cursor from a previous search page")
		}
		query.Set("cursor", cursor)
	}
	return query, cql, nil
}

// decodePageSummary reports isPage false for results of another content type, which are skipped.
func decodePageSummary(resource searchContentResource, siteBase string) (PageSummary, bool, error) {
	if resource.Type != "page" {
		return PageSummary{}, false, nil
	}
	if !contentIDPattern.MatchString(resource.ID) || resource.Title == "" {
		return PageSummary{}, true, errors.New("page ID or title is missing")
	}
	summary := PageSummary{
		ID: resource.ID, Title: resource.Title, Status: resource.Status,
		WebURL: buildWebURL(siteBase, resource.Links.WebUI),
	}
	if resource.Space != nil {
		if spaceID := resource.Space.ID.String(); contentIDPattern.MatchString(spaceID) {
			summary.SpaceID = spaceID
		}
		if spaceKeyPattern.MatchString(resource.Space.Key) {
			summary.SpaceKey = resource.Space.Key
		}
	}
	if resource.Version != nil {
		summary.VersionNumber = resource.Version.Number
		// An unparseable time leaves LastModifiedAt zero; the page is still a match.
		summary.LastModifiedAt, _ = time.Parse(time.RFC3339Nano, resource.Version.When)
	}
	return summary, true, nil
}

// isModifiedBefore removes pages the widened CQL date bound let through; an unknown time is kept.
func isModifiedBefore(summary PageSummary, modifiedSince *time.Time) bool {
	return modifiedSince != nil && !summary.LastModifiedAt.IsZero() && summary.LastModifiedAt.Before(*modifiedSince)
}

// readNextCursor extracts the cursor from Confluence's relative next link.
func readNextCursor(nextLink string) (string, error) {
	if nextLink == "" {
		return "", nil
	}
	parsed, err := url.Parse(nextLink)
	if err != nil {
		return "", err
	}
	cursor := parsed.Query().Get("cursor")
	if !isCursor(cursor) {
		return "", errors.New("cursor is missing or invalid")
	}
	return cursor, nil
}

func isCursor(value string) bool {
	if value == "" || len(value) > maximumCursorBytes {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < 0x21 || value[index] > 0x7e {
			return false
		}
	}
	return true
}
