// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// DefaultSearchPageSize is the page size used when SearchTicketsInput.PageSize is zero.
	DefaultSearchPageSize = 25
	// MaxSearchPageSize is the largest accepted page size; Zendesk recommends 100 for export search.
	MaxSearchPageSize = 100

	maximumSearchTextWords = 32
	maximumCursorLength    = 2048
	searchTicketsOperation = "searchTickets"
)

// SearchTicketsInput selects tickets with typed filters that the connector turns
// into one Zendesk search query. Different filters narrow each other; several
// statuses or several tags match any of them. At least one filter is required.
type SearchTicketsInput struct {
	// Statuses matches tickets in any of these Zendesk statuses. Empty matches every status.
	Statuses []TicketStatus `json:"statuses,omitempty"`
	// RequesterEmail matches tickets whose requester has this email address, such as jane@example.com.
	RequesterEmail string `json:"requesterEmail,omitempty"`
	// Tags matches tickets carrying any of these lowercase tags.
	Tags []string `json:"tags,omitempty"`
	// UpdatedSince matches tickets updated at or after this RFC 3339 instant with an explicit
	// offset, such as 2026-01-21T00:00:00Z. Blank applies no time filter.
	UpdatedSince string `json:"updatedSince,omitempty"`
	// Text holds up to 32 plain keywords that must all appear in the ticket's subject,
	// description, or comments. Search operators such as : < > = " * and a leading - or +
	// are rejected, so the text cannot change the other filters. Blank matches any text.
	Text string `json:"text,omitempty"`
	// PageSize is 1 to MaxSearchPageSize tickets; zero uses DefaultSearchPageSize.
	PageSize int `json:"pageSize,omitempty"`
	// Cursor is a previous page's NextCursor. Zendesk expires it one hour after that page was returned.
	Cursor string `json:"cursor,omitempty"`
}

// SearchTicketsOutput is one page of matching tickets. Zendesk's export search orders
// results by creation time and indexes new or changed tickets within a few minutes,
// so a page can omit a change made moments earlier.
type SearchTicketsOutput struct {
	// Tickets lists the matching tickets without their Description.
	Tickets []Ticket `json:"tickets"`
	// NextCursor reads the next page; it is empty on the last page.
	NextCursor string `json:"nextCursor,omitempty"`
}

// SearchTicketsOperation is the searchTickets Query.
type SearchTicketsOperation struct {
	client *Client
}

type searchExportResponseWire struct {
	Results []json.RawMessage `json:"results"`
	Meta    struct {
		HasMore     bool   `json:"has_more"`
		AfterCursor string `json:"after_cursor"`
	} `json:"meta"`
}

// Definition returns the immutable connector operation definition.
func (SearchTicketsOperation) Definition() sdkgo.QueryDefinition { return SearchTicketsDefinition }

// Invoke reads one page from Zendesk's cursor-based export search, filtered to tickets.
func (operation SearchTicketsOperation) Invoke(call sdkgo.Call, input SearchTicketsInput) sdkgo.QueryAttempt[SearchTicketsOutput] {
	query, err := BuildTicketSearchQuery(input)
	if err == nil {
		err = validateSearchPage(input)
	}
	if err != nil {
		return sdkgo.NewQueryBranch(SearchTicketsBranchDefect, SearchTicketsOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, searchTicketsOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, searchTicketsOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(SearchTicketsBranchDefect, SearchTicketsOutput{}, failure, sdkgo.Receipt{})
	}
	parameters := url.Values{
		"query":        {query},
		"filter[type]": {"ticket"},
		"page[size]":   {strconv.Itoa(searchPageSize(input))},
	}
	if input.Cursor != "" {
		parameters.Set("page[after]", input.Cursor)
	}
	result := operation.client.exchange(call, credentials, searchTicketsOperation, zendeskRequest{method: http.MethodGet, path: "/search/export", query: parameters})
	receipt := operation.client.receipt(call, result.response, 0)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRetryable:
		return sdkgo.NewQueryRetry[SearchTicketsOutput](result.failure, result.retryAfter)
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(SearchTicketsBranchInvalidResponse, SearchTicketsOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewQueryBranch(SearchTicketsBranchDefect, SearchTicketsOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(SearchTicketsBranchProviderRejected, SearchTicketsOutput{}, &result.failure, receipt)
	}
	output, err := decodeSearchPage(result.response.body, operation.client.agentTicketURL)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchTicketsBranchInvalidResponse, SearchTicketsOutput{}, zendeskFailurePointer(sdkgo.FailureProtocol, searchTicketsOperation, "Zendesk returned an invalid search page: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(SearchTicketsBranchSearched, output, nil, receipt)
}

// BuildTicketSearchQuery returns the Zendesk search query searchTickets sends, such as
// status:open status:pending requester:jane@example.com tags:billing updated>=2026-01-21T00:00:00Z.
// Export search selects tickets with its own type filter, so the query never contains type:.
func BuildTicketSearchQuery(input SearchTicketsInput) (string, error) {
	var terms []string
	seenStatuses := make(map[TicketStatus]bool, len(input.Statuses))
	for _, status := range input.Statuses {
		if err := validateTicketStatus(status); err != nil {
			return "", err
		}
		if seenStatuses[status] {
			return "", fmt.Errorf("statuses lists %q twice", status)
		}
		seenStatuses[status] = true
		terms = append(terms, "status:"+string(status))
	}
	if input.RequesterEmail != "" {
		if !isBareEmailAddress(input.RequesterEmail) {
			return "", errors.New("requesterEmail must be one bare email address such as jane@example.com")
		}
		terms = append(terms, "requester:"+input.RequesterEmail)
	}
	if err := validateTags("tags", input.Tags); err != nil {
		return "", err
	}
	for _, tag := range input.Tags {
		terms = append(terms, "tags:"+tag)
	}
	if input.UpdatedSince != "" {
		updatedSince, err := time.Parse(time.RFC3339, input.UpdatedSince)
		if err != nil {
			return "", errors.New("updatedSince must be an RFC 3339 instant with an explicit offset, such as 2026-01-21T00:00:00Z")
		}
		terms = append(terms, "updated>="+updatedSince.UTC().Format(time.RFC3339))
	}
	words := strings.Fields(input.Text)
	if len(words) > maximumSearchTextWords {
		return "", fmt.Errorf("text has more than %d keywords", maximumSearchTextWords)
	}
	for _, word := range words {
		if strings.ContainsAny(word, `:<>="*`) || strings.HasPrefix(word, "-") || strings.HasPrefix(word, "+") {
			return "", fmt.Errorf("text keyword %q contains a search operator", word)
		}
		terms = append(terms, word)
	}
	if len(terms) == 0 {
		return "", errors.New("at least one of statuses, requesterEmail, tags, updatedSince, or text is required")
	}
	return strings.Join(terms, " "), nil
}

func validateSearchPage(input SearchTicketsInput) error {
	if input.PageSize < 0 || input.PageSize > MaxSearchPageSize {
		return fmt.Errorf("pageSize must be between 1 and %d, or zero for %d", MaxSearchPageSize, DefaultSearchPageSize)
	}
	if len(input.Cursor) > maximumCursorLength {
		return errors.New("cursor is longer than any Zendesk cursor")
	}
	for index := 0; index < len(input.Cursor); index++ {
		if input.Cursor[index] <= ' ' || input.Cursor[index] > '~' {
			return errors.New("cursor must be the printable NextCursor of a previous page")
		}
	}
	return nil
}

func searchPageSize(input SearchTicketsInput) int {
	if input.PageSize == 0 {
		return DefaultSearchPageSize
	}
	return input.PageSize
}

func decodeSearchPage(body []byte, agentTicketURL string) (SearchTicketsOutput, error) {
	var page searchExportResponseWire
	if err := json.Unmarshal(body, &page); err != nil {
		return SearchTicketsOutput{}, errors.New("search page is not JSON")
	}
	if page.Results == nil {
		return SearchTicketsOutput{}, errors.New("search page has no results array")
	}
	output := SearchTicketsOutput{Tickets: make([]Ticket, 0, len(page.Results))}
	for index, raw := range page.Results {
		var resultType struct {
			ResultType string `json:"result_type"`
		}
		if err := json.Unmarshal(raw, &resultType); err != nil {
			return SearchTicketsOutput{}, fmt.Errorf("search result %d is not an object", index)
		}
		if resultType.ResultType != "" && resultType.ResultType != "ticket" {
			return SearchTicketsOutput{}, fmt.Errorf("search result %d is not a ticket", index)
		}
		var wire zendeskTicketWire
		if err := json.Unmarshal(raw, &wire); err != nil {
			return SearchTicketsOutput{}, fmt.Errorf("search result %d is not a ticket", index)
		}
		decoded, err := decodeTicketWire(wire, agentTicketURL)
		if err != nil {
			return SearchTicketsOutput{}, fmt.Errorf("search result %d: %w", index, err)
		}
		decoded.ticket.Description = ""
		decoded.ticket.IsDescriptionTruncated = false
		output.Tickets = append(output.Tickets, decoded.ticket)
	}
	if page.Meta.HasMore {
		if page.Meta.AfterCursor == "" {
			return SearchTicketsOutput{}, errors.New("search page has more results but no cursor")
		}
		output.NextCursor = page.Meta.AfterCursor
	}
	return output, nil
}
