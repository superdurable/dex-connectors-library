// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	defaultSearchPageSize      = 50
	maximumSearchPageSize      = 100
	maximumNextPageTokenBytes  = 4096
	searchIssuesOperationID    = "searchIssues"
	enhancedSearchPath         = "/search/jql"
	searchIssuesFailureSubject = "issue search"
)

// SearchIssuesInput selects one page of issues. Set exactly one of Filter and JQL.
type SearchIssuesInput struct {
	// Filter is a typed search the connector escapes into bounded JQL. Prefer it to JQL.
	Filter *IssueSearchFilter `json:"filter,omitempty"`
	// JQL is a caller-built bounded query, such as project = "OPS" ORDER BY created DESC. Quote every
	// value that came from outside the application with QuoteJQLString.
	JQL string `json:"jql,omitempty"`
	// AdditionalFields lists up to 20 more field IDs to return raw, such as customfield_10020 or duedate.
	AdditionalFields []string `json:"additionalFields,omitempty"`
	// PageSize is the number of issues to request, 1 to 100; zero requests 50. Jira may return fewer.
	PageSize int `json:"pageSize,omitempty"`
	// NextPageToken continues a previous search with the same query; blank reads the first page.
	// Jira expires a token after seven days.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// SearchIssuesOutput is one page of matching issues.
type SearchIssuesOutput struct {
	// JQL is the exact query sent to Jira.
	JQL string `json:"jql"`
	// Issues lists the page's issues with their standard fields and requested additional fields.
	Issues []Issue `json:"issues"`
	// NextPageToken continues the search; it is empty on the last page.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// SearchIssuesOperation implements the searchIssues Query with Jira's enhanced search endpoint.
type SearchIssuesOperation struct{ client *Client }

type searchRequestBody struct {
	JQL           string   `json:"jql"`
	Fields        []string `json:"fields"`
	MaxResults    int      `json:"maxResults"`
	NextPageToken string   `json:"nextPageToken,omitempty"`
}

type searchResponseBody struct {
	Issues        []issueResource `json:"issues"`
	NextPageToken string          `json:"nextPageToken"`
}

// Definition returns the immutable connector operation definition.
func (SearchIssuesOperation) Definition() sdkgo.QueryDefinition { return SearchIssuesDefinition }

// Invoke reads one search page. Transport failures, 408, 429, and 5xx responses are retried.
func (operation SearchIssuesOperation) Invoke(call sdkgo.Call, input SearchIssuesInput) sdkgo.QueryAttempt[SearchIssuesOutput] {
	client := operation.client
	body, err := buildSearchRequestBody(input)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchIssuesBranchDefect, SearchIssuesOutput{}, failurePointer(searchIssuesOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, sessionErr := client.startSession(call, searchIssuesOperationID)
	if sessionErr != nil {
		if sessionErr.isRetryable {
			return sdkgo.NewQueryRetry[SearchIssuesOutput](sessionErr.failure, sessionErr.retryAfter)
		}
		return sdkgo.NewQueryBranch(SearchIssuesBranchDefect, SearchIssuesOutput{}, sessionErr.pointer(), sdkgo.Receipt{})
	}
	defer cancel()
	result := client.exchange(session, jiraRequest{method: http.MethodPost, path: enhancedSearchPath, payload: body})
	output := SearchIssuesOutput{JQL: body.JQL}
	classification := client.classifyRead(searchIssuesOperationID, searchIssuesFailureSubject, result)
	receipt := client.receipt(session, result.response, "")
	switch classification.outcome {
	case readSucceeded:
	case readRetry:
		return sdkgo.NewQueryRetry[SearchIssuesOutput](classification.failure, classification.retryAfter)
	case readDefect:
		return sdkgo.NewQueryBranch(SearchIssuesBranchDefect, output, &classification.failure, receipt)
	case readInvalid:
		return sdkgo.NewQueryBranch(SearchIssuesBranchInvalidResponse, output, &classification.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(SearchIssuesBranchProviderRejected, output, &classification.failure, receipt)
	}
	var page searchResponseBody
	if err := json.Unmarshal(result.response.body, &page); err != nil || len(page.NextPageToken) > maximumNextPageTokenBytes {
		return sdkgo.NewQueryBranch(SearchIssuesBranchInvalidResponse, output, failurePointer(searchIssuesOperationID, sdkgo.FailureProtocol, "Jira returned an invalid search page"), receipt)
	}
	output.Issues = make([]Issue, 0, len(page.Issues))
	for _, resource := range page.Issues {
		issue, err := decodeIssue(resource, input.AdditionalFields, false)
		if err != nil {
			return sdkgo.NewQueryBranch(SearchIssuesBranchInvalidResponse, SearchIssuesOutput{JQL: body.JQL},
				failurePointer(searchIssuesOperationID, sdkgo.FailureProtocol, "Jira returned an invalid issue in the search page: "+err.Error()), receipt)
		}
		output.Issues = append(output.Issues, issue)
	}
	output.NextPageToken = page.NextPageToken
	return sdkgo.NewQueryBranch(SearchIssuesBranchSearched, output, nil, receipt)
}

func buildSearchRequestBody(input SearchIssuesInput) (searchRequestBody, error) {
	var jql string
	var err error
	switch {
	case input.Filter != nil && strings.TrimSpace(input.JQL) != "":
		return searchRequestBody{}, errors.New("set either filter or jql, not both")
	case input.Filter != nil:
		jql, err = input.Filter.JQL()
	default:
		jql, err = validateCallerJQL(input.JQL)
	}
	if err != nil {
		return searchRequestBody{}, err
	}
	fields, err := validateAdditionalFieldIDs(input.AdditionalFields, false)
	if err != nil {
		return searchRequestBody{}, err
	}
	pageSize := input.PageSize
	switch {
	case pageSize == 0:
		pageSize = defaultSearchPageSize
	case pageSize < 1 || pageSize > maximumSearchPageSize:
		return searchRequestBody{}, errors.New("pageSize must be between 1 and 100")
	}
	pageToken := strings.TrimSpace(input.NextPageToken)
	if len(pageToken) > maximumNextPageTokenBytes || (pageToken != "" && !isPrintableASCII(pageToken)) {
		return searchRequestBody{}, errors.New("nextPageToken must be the printable token from a previous search page")
	}
	return searchRequestBody{JQL: jql, Fields: fields, MaxResults: pageSize, NextPageToken: pageToken}, nil
}

func isPrintableASCII(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] < 0x21 || value[index] > 0x7e {
			return false
		}
	}
	return true
}
