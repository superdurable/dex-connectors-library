// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// DefaultSearchPageSize is the page size used when SearchTicketsInput.PageSize is zero; it is Gorgias's default.
	DefaultSearchPageSize = 30
	// MaxSearchPageSize is Gorgias's largest page size for listing tickets.
	MaxSearchPageSize = 100

	searchTicketsOperation = "searchTickets"
	maximumCursorLength    = 2048
)

// cursorPattern accepts Gorgias's base64 cursors, so a cursor can never change the request's other parameters.
var cursorPattern = regexp.MustCompile(`^[A-Za-z0-9+/=_.:-]+$`)

func isValidCursor(cursor string) bool {
	return len(cursor) <= maximumCursorLength && cursorPattern.MatchString(cursor)
}

// SearchTicketsInput lists tickets most recently updated first. RequesterEmail, RequesterID, and
// ViewID narrow the list on Gorgias's side; Statuses, Priorities, Tags, and UpdatedSince filter
// each page after Gorgias returns it, so a page can hold fewer than PageSize tickets, even none,
// while NextCursor is set. Several statuses, priorities, or tags match any of them.
type SearchTicketsInput struct {
	// RequesterEmail lists only the tickets of the customer whose primary email address this is,
	// such as jane@example.com. A customer that does not exist yields an empty page.
	RequesterEmail string `json:"requesterEmail,omitempty"`
	// RequesterID lists only the tickets of this Gorgias customer. Use it or RequesterEmail, not both.
	RequesterID int64 `json:"requesterId,omitempty"`
	// ViewID lists only the tickets matching the filters of this Gorgias view, such as one an
	// administrator built for a status and tag; Gorgias then orders the page by the view's sort.
	ViewID int64 `json:"viewId,omitempty"`
	// Statuses keeps tickets in any of these Gorgias statuses. Empty keeps every status.
	Statuses []TicketStatus `json:"statuses,omitempty"`
	// Priorities keeps tickets with any of these Gorgias priorities. Empty keeps every priority.
	Priorities []TicketPriority `json:"priorities,omitempty"`
	// Tags keeps tickets carrying any of these tag names, compared case sensitively as Gorgias does.
	Tags []string `json:"tags,omitempty"`
	// UpdatedSince keeps tickets updated at or after this RFC 3339 instant with an explicit offset,
	// such as 2026-01-21T00:00:00Z. Without ViewID it also ends paging at the first older ticket.
	UpdatedSince string `json:"updatedSince,omitempty"`
	// PageSize is 1 to MaxSearchPageSize tickets listed from Gorgias; zero uses DefaultSearchPageSize.
	PageSize int `json:"pageSize,omitempty"`
	// Cursor is a previous page's NextCursor, read with the same filters.
	Cursor string `json:"cursor,omitempty"`
}

// SearchTicketsOutput is one page of matching tickets.
type SearchTicketsOutput struct {
	// Tickets lists the matching tickets in Gorgias's order, without their messages.
	Tickets []Ticket `json:"tickets"`
	// NextCursor reads the next page; it is empty on the last page.
	NextCursor string `json:"nextCursor,omitempty"`
	// ListedCount is the number of tickets Gorgias listed on this page before the connector's filters.
	ListedCount int `json:"listedCount"`
}

// SearchTicketsOperation is the searchTickets Query.
type SearchTicketsOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (SearchTicketsOperation) Definition() sdkgo.QueryDefinition { return SearchTicketsDefinition }

// Invoke looks up the requester's customer when RequesterEmail is set, then reads one page of
// GET /api/tickets ordered by update time, excluding trashed tickets.
func (operation SearchTicketsOperation) Invoke(call sdkgo.Call, input SearchTicketsInput) sdkgo.QueryAttempt[SearchTicketsOutput] {
	updatedSince, err := validateSearchTicketsInput(input)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchTicketsBranchDefect, SearchTicketsOutput{}, gorgiasFailurePointer(sdkgo.FailureValidation, searchTicketsOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, searchTicketsOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(SearchTicketsBranchDefect, SearchTicketsOutput{}, failure, sdkgo.Receipt{})
	}
	requesterID := input.RequesterID
	if input.RequesterEmail != "" {
		lookup := exchangeCustomerLookup(operation.client, call, credentials, searchTicketsOperation, input.RequesterEmail)
		receipt := operation.client.receipt(call, lookup.response, 0)
		if attempt, isTerminal := searchTicketsAttemptForExchange(lookup, receipt); isTerminal {
			return attempt
		}
		matches, err := decodeCustomerMatches(lookup.response.body, input.RequesterEmail)
		if err == nil && len(matches.Customers) > 1 {
			err = errors.New("more than one customer has this primary email address")
		}
		if err != nil {
			return sdkgo.NewQueryBranch(SearchTicketsBranchInvalidResponse, SearchTicketsOutput{}, gorgiasFailurePointer(sdkgo.FailureProtocol, searchTicketsOperation, "Gorgias returned an invalid customer page: "+err.Error()), receipt)
		}
		if len(matches.Customers) == 0 {
			return sdkgo.NewQueryBranch(SearchTicketsBranchSearched, SearchTicketsOutput{Tickets: []Ticket{}}, nil, receipt)
		}
		requesterID = matches.Customers[0].ID
	}
	result := operation.client.exchange(call, credentials, searchTicketsOperation, gorgiasRequest{
		method: http.MethodGet, path: "/tickets", query: buildListTicketsQuery(input, requesterID),
	})
	receipt := operation.client.receipt(call, result.response, 0)
	if attempt, isTerminal := searchTicketsAttemptForExchange(result, receipt); isTerminal {
		return attempt
	}
	output, err := decodeTicketPage(result.response.body, input, updatedSince)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchTicketsBranchInvalidResponse, SearchTicketsOutput{}, gorgiasFailurePointer(sdkgo.FailureProtocol, searchTicketsOperation, "Gorgias returned an invalid ticket page: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(SearchTicketsBranchSearched, output, nil, receipt)
}

// searchTicketsAttemptForExchange returns the terminal attempt for every outcome except success.
func searchTicketsAttemptForExchange(result gorgiasExchange, receipt sdkgo.Receipt) (sdkgo.QueryAttempt[SearchTicketsOutput], bool) {
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

// validateSearchTicketsInput returns the parsed UpdatedSince, or nil when it is blank.
func validateSearchTicketsInput(input SearchTicketsInput) (*time.Time, error) {
	if input.RequesterEmail != "" && !isBareEmailAddress(input.RequesterEmail) {
		return nil, errors.New("requesterEmail must be one bare email address such as jane@example.com")
	}
	if input.RequesterEmail != "" && input.RequesterID != 0 {
		return nil, errors.New("set requesterEmail or requesterId, not both")
	}
	if err := errors.Join(validateOptionalID("requesterId", input.RequesterID), validateOptionalID("viewId", input.ViewID), validateTags("tags", input.Tags)); err != nil {
		return nil, err
	}
	for index, status := range input.Statuses {
		if err := validateTicketStatus("statuses entry", status); err != nil {
			return nil, err
		}
		if slices.Contains(input.Statuses[:index], status) {
			return nil, fmt.Errorf("statuses lists %q twice", status)
		}
	}
	for index, priority := range input.Priorities {
		if err := validateTicketPriority("priorities entry", priority); err != nil {
			return nil, err
		}
		if slices.Contains(input.Priorities[:index], priority) {
			return nil, fmt.Errorf("priorities lists %q twice", priority)
		}
	}
	if input.PageSize < 0 || input.PageSize > MaxSearchPageSize {
		return nil, fmt.Errorf("pageSize must be between 1 and %d, or zero for %d", MaxSearchPageSize, DefaultSearchPageSize)
	}
	if input.Cursor != "" && !isValidCursor(input.Cursor) {
		return nil, errors.New("cursor must be a nextCursor value returned by searchTickets")
	}
	if input.UpdatedSince == "" {
		return nil, nil
	}
	updatedSince, err := time.Parse(time.RFC3339, input.UpdatedSince)
	if err != nil {
		return nil, errors.New("updatedSince must be an RFC 3339 instant with an explicit offset, such as 2026-01-21T00:00:00Z")
	}
	updatedSince = updatedSince.UTC()
	return &updatedSince, nil
}

func buildListTicketsQuery(input SearchTicketsInput, requesterID int64) url.Values {
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = DefaultSearchPageSize
	}
	query := url.Values{"order_by": {"updated_datetime:desc"}, "limit": {strconv.Itoa(pageSize)}, "trashed": {"false"}}
	if requesterID > 0 {
		query.Set("customer_id", strconv.FormatInt(requesterID, 10))
	}
	if input.ViewID > 0 {
		query.Set("view_id", strconv.FormatInt(input.ViewID, 10))
	}
	if input.Cursor != "" {
		query.Set("cursor", input.Cursor)
	}
	return query
}

// decodeTicketPage applies the page filters; without a view, the first ticket older than UpdatedSince ends paging.
func decodeTicketPage(body []byte, input SearchTicketsInput, updatedSince *time.Time) (SearchTicketsOutput, error) {
	wires, nextCursor, err := decodeListBody[gorgiasTicketWire](body, "ticket")
	if err != nil {
		return SearchTicketsOutput{}, err
	}
	if len(wires) > MaxSearchPageSize {
		return SearchTicketsOutput{}, errors.New("ticket page holds more tickets than Gorgias's page size")
	}
	output := SearchTicketsOutput{Tickets: []Ticket{}, NextCursor: nextCursor, ListedCount: len(wires)}
	for index, wire := range wires {
		ticket, err := decodeTicketWire(wire)
		if err != nil {
			return SearchTicketsOutput{}, fmt.Errorf("ticket %d: %w", index, err)
		}
		if updatedSince != nil && ticket.UpdatedAt.Before(*updatedSince) {
			if input.ViewID == 0 {
				output.NextCursor = ""
			}
			continue
		}
		if isTicketSelected(ticket, input) {
			output.Tickets = append(output.Tickets, ticket)
		}
	}
	return output, nil
}

func isTicketSelected(ticket Ticket, input SearchTicketsInput) bool {
	if len(input.Statuses) != 0 && !slices.Contains(input.Statuses, ticket.Status) {
		return false
	}
	if len(input.Priorities) != 0 && !slices.Contains(input.Priorities, ticket.Priority) {
		return false
	}
	return len(input.Tags) == 0 || slices.ContainsFunc(input.Tags, func(tag string) bool { return slices.Contains(ticket.Tags, tag) })
}
