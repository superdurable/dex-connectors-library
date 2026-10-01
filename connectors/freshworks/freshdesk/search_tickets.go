// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// SearchPageSize is the fixed number of tickets per page of Freshdesk's filter query.
	SearchPageSize = 30
	// MaxSearchPage is the last page Freshdesk's filter query serves, so a search reaches at most 300 tickets.
	MaxSearchPage = 10
	// MaxFilterQueryLength is Freshdesk's limit for the filter query, including its enclosing double quotes.
	MaxFilterQueryLength = 512

	updatedSinceDateLayout = "2006-01-02"
	searchTicketsOperation = "searchTickets"
)

// SearchTicketsInput selects tickets with typed filters that the connector turns into one
// Freshdesk filter query. Different filters narrow each other; several statuses, priorities,
// or tags match any of them. At least one of Statuses, Priorities, Tags, or UpdatedSinceDate is
// required, because Freshdesk's filter query needs a condition and has no requester field.
type SearchTicketsInput struct {
	// Statuses matches tickets in any of these Freshdesk statuses, including custom ones. Empty matches every status.
	Statuses []TicketStatus `json:"statuses,omitempty"`
	// Priorities matches tickets with any of these Freshdesk priorities. Empty matches every priority.
	Priorities []TicketPriority `json:"priorities,omitempty"`
	// Tags matches tickets carrying any of these tags.
	Tags []string `json:"tags,omitempty"`
	// UpdatedSinceDate matches tickets updated on or after this UTC calendar date, written YYYY-MM-DD.
	// Freshdesk's filter query compares dates, not instants. Blank applies no time filter.
	UpdatedSinceDate string `json:"updatedSinceDate,omitempty"`
	// RequesterEmail keeps only tickets requested by the contact with this email address, such as
	// jane@example.com. The connector looks the contact up first and filters each page itself, so a
	// page can hold fewer than SearchPageSize tickets, even none, while NextPage is set. Blank keeps
	// every requester.
	RequesterEmail string `json:"requesterEmail,omitempty"`
	// Page is 1 to MaxSearchPage; zero reads the first page.
	Page int `json:"page,omitempty"`
}

// SearchTicketsOutput is one page of matching tickets. Freshdesk indexes new and changed tickets
// within a few minutes, so a page can omit a change made moments earlier.
type SearchTicketsOutput struct {
	// Tickets lists the matching tickets without their Description, and possibly without Tags.
	Tickets []Ticket `json:"tickets"`
	// TotalMatched is Freshdesk's count of tickets matching the filter query, before the
	// RequesterEmail filter; Freshdesk serves only the first 300 of them.
	TotalMatched int `json:"totalMatched"`
	// NextPage is the page to read next, or zero when this is the last page Freshdesk serves.
	NextPage int `json:"nextPage,omitempty"`
}

// SearchTicketsOperation is the searchTickets Query.
type SearchTicketsOperation struct {
	client *Client
}

type filterQueryResponseWire struct {
	Total   *int                  `json:"total"`
	Results []freshdeskTicketWire `json:"results"`
}

// Definition returns the immutable connector operation definition.
func (SearchTicketsOperation) Definition() sdkgo.QueryDefinition { return SearchTicketsDefinition }

// Invoke looks up the requester's contact when RequesterEmail is set, then reads one page of
// Freshdesk's filter query, GET /api/v2/search/tickets.
func (operation SearchTicketsOperation) Invoke(call sdkgo.Call, input SearchTicketsInput) sdkgo.QueryAttempt[SearchTicketsOutput] {
	query, err := BuildTicketFilterQuery(input)
	if err == nil {
		err = validateSearchRequester(input)
	}
	if err != nil {
		return sdkgo.NewQueryBranch(SearchTicketsBranchDefect, SearchTicketsOutput{}, freshdeskFailurePointer(sdkgo.FailureValidation, searchTicketsOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, searchTicketsOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(SearchTicketsBranchDefect, SearchTicketsOutput{}, failure, sdkgo.Receipt{})
	}
	var requesterIDs map[int64]bool
	if input.RequesterEmail != "" {
		contactResult := operation.client.exchange(call, credentials, searchTicketsOperation, freshdeskRequest{
			method: http.MethodGet, path: "/contacts", query: url.Values{"email": {input.RequesterEmail}},
		})
		receipt := operation.client.receipt(call, contactResult.response, 0)
		if attempt, isTerminal := searchTicketsAttemptForExchange(contactResult, receipt); isTerminal {
			return attempt
		}
		requesterIDs, err = decodeContactIDs(contactResult.response.body, input.RequesterEmail)
		if err != nil {
			return sdkgo.NewQueryBranch(SearchTicketsBranchInvalidResponse, SearchTicketsOutput{}, freshdeskFailurePointer(sdkgo.FailureProtocol, searchTicketsOperation, "Freshdesk returned an invalid contact list: "+err.Error()), receipt)
		}
		if len(requesterIDs) == 0 {
			return sdkgo.NewQueryBranch(SearchTicketsBranchSearched, SearchTicketsOutput{Tickets: []Ticket{}}, nil, receipt)
		}
	}
	page := searchPage(input)
	result := operation.client.exchange(call, credentials, searchTicketsOperation, freshdeskRequest{
		method: http.MethodGet, path: "/search/tickets", query: url.Values{"query": {`"` + query + `"`}, "page": {strconv.Itoa(page)}},
	})
	receipt := operation.client.receipt(call, result.response, 0)
	if attempt, isTerminal := searchTicketsAttemptForExchange(result, receipt); isTerminal {
		return attempt
	}
	output, err := decodeFilterQueryPage(result.response.body, page, requesterIDs)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchTicketsBranchInvalidResponse, SearchTicketsOutput{}, freshdeskFailurePointer(sdkgo.FailureProtocol, searchTicketsOperation, "Freshdesk returned an invalid search page: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(SearchTicketsBranchSearched, output, nil, receipt)
}

// searchTicketsAttemptForExchange returns the terminal attempt for every outcome except success.
func searchTicketsAttemptForExchange(result freshdeskExchange, receipt sdkgo.Receipt) (sdkgo.QueryAttempt[SearchTicketsOutput], bool) {
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.QueryAttempt[SearchTicketsOutput]{}, false
	case exchangeRateLimited, exchangeNotSent, exchangeUnavailable:
		return sdkgo.NewQueryRetry[SearchTicketsOutput](result.failure, result.retryAfter), true
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(SearchTicketsBranchInvalidResponse, SearchTicketsOutput{}, &result.failure, receipt), true
	case exchangeDefect:
		return sdkgo.NewQueryBranch(SearchTicketsBranchDefect, SearchTicketsOutput{}, &result.failure, receipt), true
	default:
		return sdkgo.NewQueryBranch(SearchTicketsBranchProviderRejected, SearchTicketsOutput{}, &result.failure, receipt), true
	}
}

// BuildTicketFilterQuery returns the Freshdesk filter query searchTickets sends between double
// quotes, such as (status:2 OR status:3) AND tag:'billing' AND updated_at:>'2026-01-21'.
// RequesterEmail is not part of the query, because Freshdesk's filter query has no requester field.
func BuildTicketFilterQuery(input SearchTicketsInput) (string, error) {
	var conditions []string
	statusTerms := make([]string, 0, len(input.Statuses))
	seenStatuses := make(map[TicketStatus]bool, len(input.Statuses))
	for _, status := range input.Statuses {
		if err := validateTicketStatus("statuses entry", status); err != nil {
			return "", err
		}
		if seenStatuses[status] {
			return "", fmt.Errorf("statuses lists %d twice", status)
		}
		seenStatuses[status] = true
		statusTerms = append(statusTerms, "status:"+strconv.Itoa(int(status)))
	}
	conditions = appendAnyOfCondition(conditions, statusTerms)
	priorityTerms := make([]string, 0, len(input.Priorities))
	seenPriorities := make(map[TicketPriority]bool, len(input.Priorities))
	for _, priority := range input.Priorities {
		if err := validateTicketPriority("priorities entry", priority); err != nil {
			return "", err
		}
		if seenPriorities[priority] {
			return "", fmt.Errorf("priorities lists %d twice", priority)
		}
		seenPriorities[priority] = true
		priorityTerms = append(priorityTerms, "priority:"+strconv.Itoa(int(priority)))
	}
	conditions = appendAnyOfCondition(conditions, priorityTerms)
	if err := validateTags("tags", input.Tags); err != nil {
		return "", err
	}
	tagTerms := make([]string, 0, len(input.Tags))
	for _, tag := range input.Tags {
		tagTerms = append(tagTerms, "tag:'"+tag+"'")
	}
	conditions = appendAnyOfCondition(conditions, tagTerms)
	if input.UpdatedSinceDate != "" {
		date, err := time.Parse(updatedSinceDateLayout, input.UpdatedSinceDate)
		if err != nil || date.Format(updatedSinceDateLayout) != input.UpdatedSinceDate {
			return "", errors.New("updatedSinceDate must be a UTC calendar date written YYYY-MM-DD, such as 2026-01-21")
		}
		conditions = append(conditions, "updated_at:>'"+input.UpdatedSinceDate+"'")
	}
	if len(conditions) == 0 {
		return "", errors.New("at least one of statuses, priorities, tags, or updatedSinceDate is required")
	}
	query := strings.Join(conditions, " AND ")
	if len(query)+2 > MaxFilterQueryLength {
		return "", fmt.Errorf("the filter query is longer than Freshdesk's %d-character limit; use fewer statuses, priorities, or tags", MaxFilterQueryLength)
	}
	return query, nil
}

// appendAnyOfCondition joins terms with OR, parenthesized when there are several.
func appendAnyOfCondition(conditions []string, terms []string) []string {
	switch len(terms) {
	case 0:
		return conditions
	case 1:
		return append(conditions, terms[0])
	default:
		return append(conditions, "("+strings.Join(terms, " OR ")+")")
	}
}

func validateSearchRequester(input SearchTicketsInput) error {
	if input.Page < 0 || input.Page > MaxSearchPage {
		return fmt.Errorf("page must be between 1 and %d, or zero for the first page", MaxSearchPage)
	}
	if input.RequesterEmail != "" && !isBareEmailAddress(input.RequesterEmail) {
		return errors.New("requesterEmail must be one bare email address such as jane@example.com")
	}
	return nil
}

func searchPage(input SearchTicketsInput) int {
	if input.Page == 0 {
		return 1
	}
	return input.Page
}

// decodeContactIDs keeps only contacts whose email equals the requested address, ignoring case.
func decodeContactIDs(body []byte, email string) (map[int64]bool, error) {
	var contacts []freshdeskRequesterWire
	if err := json.Unmarshal(body, &contacts); err != nil {
		return nil, errors.New("contact list is not a JSON array of contacts")
	}
	requesterIDs := map[int64]bool{}
	for index, contact := range contacts {
		if contact.ID < 1 {
			return nil, fmt.Errorf("contact %d has no ID", index)
		}
		if strings.EqualFold(contact.Email, email) {
			requesterIDs[contact.ID] = true
		}
	}
	return requesterIDs, nil
}

func decodeFilterQueryPage(body []byte, page int, requesterIDs map[int64]bool) (SearchTicketsOutput, error) {
	var wire filterQueryResponseWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return SearchTicketsOutput{}, errors.New("search page is not JSON")
	}
	if wire.Total == nil || *wire.Total < 0 {
		return SearchTicketsOutput{}, errors.New("search page has no total")
	}
	if wire.Results == nil {
		return SearchTicketsOutput{}, errors.New("search page has no results array")
	}
	if len(wire.Results) > SearchPageSize {
		return SearchTicketsOutput{}, errors.New("search page holds more tickets than Freshdesk's page size")
	}
	output := SearchTicketsOutput{Tickets: make([]Ticket, 0, len(wire.Results)), TotalMatched: *wire.Total}
	for index, result := range wire.Results {
		ticket, err := decodeTicketWire(result)
		if err != nil {
			return SearchTicketsOutput{}, fmt.Errorf("search result %d: %w", index, err)
		}
		if requesterIDs != nil && !requesterIDs[ticket.RequesterID] {
			continue
		}
		ticket.Description, ticket.IsDescriptionTruncated = "", false
		output.Tickets = append(output.Tickets, ticket)
	}
	if page < MaxSearchPage && page*SearchPageSize < output.TotalMatched {
		output.NextPage = page + 1
	}
	return output, nil
}

func isBareEmailAddress(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Name == "" && address.Address == value && !strings.ContainsAny(value, " \"'()<>,;")
}
