// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// DefaultSearchPageSize is the page size a zero PageSize requests.
	DefaultSearchPageSize = 50
	// MaxSearchPageSize is the largest page size searchTickets accepts.
	MaxSearchPageSize           = 100
	maximumNextPageTokenBytes   = 4096
	searchTicketsOperationID    = "searchTickets"
	enhancedSearchPath          = "/search/jql"
	searchTicketsFailureSubject = "ticket search"
)

// SearchTicketsInput selects one page of one service desk's requests. ProjectKey is required; every
// other set filter narrows the search.
type SearchTicketsInput struct {
	// ProjectKey is the key of the service desk's Jira project, such as ITH, as the service desk picker
	// stores it.
	ProjectKey string `json:"projectKey"`
	// StatusCategories matches requests in any of these status categories: new, indeterminate, or done.
	StatusCategories []StatusCategoryKey `json:"statusCategories,omitempty"`
	// StatusNames matches requests in any of these workflow statuses, such as Waiting for support.
	StatusNames []string `json:"statusNames,omitempty"`
	// Labels matches requests that carry at least one of these labels.
	Labels []string `json:"labels,omitempty"`
	// ReporterAccountIDs matches requests raised for any of these customers, by Atlassian account ID.
	ReporterAccountIDs []string `json:"reporterAccountIds,omitempty"`
	// SummaryPhrase matches requests whose summary contains this phrase as Jira's text search tokenizes
	// it. Text search ignores case and punctuation, so compare the returned summaries for an exact match.
	SummaryPhrase string `json:"summaryPhrase,omitempty"`
	// UpdatedSinceDate matches requests updated on or after this calendar date, written YYYY-MM-DD, in the
	// connected account's Jira time zone. Blank applies no time filter.
	UpdatedSinceDate string `json:"updatedSinceDate,omitempty"`
	// Order selects the result order; blank uses TicketSearchOrderCreatedDescending.
	Order TicketSearchOrder `json:"order,omitempty"`
	// PageSize is 1 to MaxSearchPageSize requests; zero uses DefaultSearchPageSize. Jira may return fewer.
	PageSize int `json:"pageSize,omitempty"`
	// NextPageToken continues a previous search with the same filters; blank reads the first page.
	// Jira expires a token after seven days.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// SearchTicketsOutput is one page of matching requests.
type SearchTicketsOutput struct {
	// JQL is the exact query sent to Jira.
	JQL string `json:"jql"`
	// Tickets lists the page's requests without their Description.
	Tickets []Ticket `json:"tickets"`
	// NextPageToken continues the search; it is empty on the last page.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// SearchTicketsOperation implements the searchTickets Query with Jira's enhanced search endpoint.
type SearchTicketsOperation struct{ client *Client }

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
func (SearchTicketsOperation) Definition() sdkgo.QueryDefinition { return SearchTicketsDefinition }

// Invoke reads one search page. Transport failures, 408, 429, and 5xx responses are retried.
func (operation SearchTicketsOperation) Invoke(call sdkgo.Call, input SearchTicketsInput) sdkgo.QueryAttempt[SearchTicketsOutput] {
	client := operation.client
	body, err := buildSearchRequestBody(input)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchTicketsBranchDefect, SearchTicketsOutput{}, failurePointer(searchTicketsOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	output := SearchTicketsOutput{JQL: body.JQL, Tickets: []Ticket{}}
	session, cancel, sessionErr := client.startSession(call, searchTicketsOperationID)
	if sessionErr != nil {
		if sessionErr.isRetryable {
			return sdkgo.NewQueryRetry[SearchTicketsOutput](sessionErr.failure, sessionErr.retryAfter)
		}
		return sdkgo.NewQueryBranch(SearchTicketsBranchDefect, output, sessionErr.pointer(), sdkgo.Receipt{})
	}
	defer cancel()
	result := client.exchange(session, providerRequest{method: http.MethodPost, api: jiraPlatformAPI, path: enhancedSearchPath, payload: body})
	classification := client.classifyRead(searchTicketsOperationID, searchTicketsFailureSubject, result)
	receipt := client.receipt(session, result.response, "")
	switch classification.outcome {
	case readSucceeded:
	case readRetry:
		return sdkgo.NewQueryRetry[SearchTicketsOutput](classification.failure, classification.retryAfter)
	case readDefect:
		return sdkgo.NewQueryBranch(SearchTicketsBranchDefect, output, &classification.failure, receipt)
	case readInvalid:
		return sdkgo.NewQueryBranch(SearchTicketsBranchInvalidResponse, output, &classification.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(SearchTicketsBranchProviderRejected, output, &classification.failure, receipt)
	}
	var page searchResponseBody
	if err := json.Unmarshal(result.response.body, &page); err != nil || len(page.NextPageToken) > maximumNextPageTokenBytes {
		return sdkgo.NewQueryBranch(SearchTicketsBranchInvalidResponse, output, failurePointer(searchTicketsOperationID, sdkgo.FailureProtocol, "Jira returned an invalid search page"), receipt)
	}
	for _, resource := range page.Issues {
		ticket, err := decodeTicket(resource, false)
		if err != nil {
			return sdkgo.NewQueryBranch(SearchTicketsBranchInvalidResponse, SearchTicketsOutput{JQL: body.JQL, Tickets: []Ticket{}},
				failurePointer(searchTicketsOperationID, sdkgo.FailureProtocol, "Jira returned an invalid request in the search page: "+err.Error()), receipt)
		}
		output.Tickets = append(output.Tickets, ticket)
	}
	output.NextPageToken = page.NextPageToken
	return sdkgo.NewQueryBranch(SearchTicketsBranchSearched, output, nil, receipt)
}

func buildSearchRequestBody(input SearchTicketsInput) (searchRequestBody, error) {
	jql, err := BuildTicketSearchJQL(input)
	if err != nil {
		return searchRequestBody{}, err
	}
	pageSize := input.PageSize
	switch {
	case pageSize == 0:
		pageSize = DefaultSearchPageSize
	case pageSize < 1 || pageSize > MaxSearchPageSize:
		return searchRequestBody{}, errors.New("pageSize must be between 1 and 100")
	}
	pageToken := strings.TrimSpace(input.NextPageToken)
	if len(pageToken) > maximumNextPageTokenBytes || (pageToken != "" && !isPrintableASCII(pageToken)) {
		return searchRequestBody{}, errors.New("nextPageToken must be the printable token from a previous search page")
	}
	return searchRequestBody{JQL: jql, Fields: ticketFieldIDs, MaxResults: pageSize, NextPageToken: pageToken}, nil
}

func isPrintableASCII(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] < 0x21 || value[index] > 0x7e {
			return false
		}
	}
	return true
}
