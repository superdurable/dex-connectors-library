// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk

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
	// DefaultSearchLimit is the number of tickets searchTickets requests when Limit is zero.
	DefaultSearchLimit = 25
	// MaxSearchLimit is Zoho Desk's largest search page.
	MaxSearchLimit = 100
	// MaxSearchResults is how deep Zoho Desk's search reaches: From is 0 to 4999, and no page passes result 5000.
	MaxSearchResults = 5000

	// searchSortOrder lists the most recently modified tickets first.
	searchSortOrder        = "-modifiedTime"
	searchTicketsOperation = "searchTickets"
)

// statusTypeSearchTokens are Zoho Desk's documented search values for each status type.
var statusTypeSearchTokens = map[TicketStatusType]string{
	TicketStatusTypeOpen:   "${OPEN}",
	TicketStatusTypeOnHold: "${ONHOLD}",
	TicketStatusTypeClosed: "${CLOSED}",
}

// SearchTicketsInput selects tickets with typed filters that the connector turns into Zoho Desk
// search parameters. Different filters narrow each other; several statuses, status types, or
// priorities match any of them. At least one filter is required.
type SearchTicketsInput struct {
	// Statuses matches tickets in any of these Zoho Desk status names, including custom statuses,
	// such as Open or Waiting for Customer. Set Statuses or StatusTypes, not both.
	Statuses []TicketStatus `json:"statuses,omitempty"`
	// StatusTypes matches tickets whose status falls under any of these types, so custom statuses
	// match without listing them. Set Statuses or StatusTypes, not both.
	StatusTypes []TicketStatusType `json:"statusTypes,omitempty"`
	// Priorities matches tickets with any of these Zoho Desk priority names, such as High.
	Priorities []TicketPriority `json:"priorities,omitempty"`
	// ContactEmail keeps only tickets whose email or contact email is this address, compared without
	// regard to letter case, such as jane@example.com. Zoho Desk's email filter also matches
	// look-alike addresses, so the connector checks each ticket itself; a page can then hold fewer
	// tickets than Limit, even none, while NextFrom is set. Blank keeps every contact.
	ContactEmail string `json:"contactEmail,omitempty"`
	// DepartmentID matches tickets in this department. Blank searches every department the agent can see.
	DepartmentID string `json:"departmentId,omitempty"`
	// ModifiedSince matches tickets modified at or after this RFC 3339 instant with an explicit
	// offset, such as 2026-01-21T00:00:00Z, until the time of the request. Blank applies no time filter.
	ModifiedSince string `json:"modifiedSince,omitempty"`
	// From is the zero-based index of the first result, 0 to 4999.
	From int `json:"from,omitempty"`
	// Limit is 1 to MaxSearchLimit; zero uses DefaultSearchLimit. A page that would pass result
	// MaxSearchResults is shortened to end there.
	Limit int `json:"limit,omitempty"`
}

// SearchTicketsOutput is one page of matching tickets, most recently modified first. Zoho Desk
// indexes new and changed tickets with a delay, so a page can omit a change made moments earlier.
type SearchTicketsOutput struct {
	// Tickets lists the matching tickets without their DescriptionHTML.
	Tickets []Ticket `json:"tickets"`
	// TotalMatched is Zoho Desk's reported count of matching tickets, before the ContactEmail check.
	TotalMatched int `json:"totalMatched"`
	// NextFrom is the From of the next page, or zero when this page is the last that Zoho Desk serves.
	NextFrom int `json:"nextFrom,omitempty"`
}

// SearchTicketsOperation is the searchTickets Query.
type SearchTicketsOperation struct {
	client *Client
}

type searchPageWire struct {
	Data  *[]deskTicketWire `json:"data"`
	Count zohoCount         `json:"count"`
}

// Definition returns the immutable connector operation definition.
func (SearchTicketsOperation) Definition() sdkgo.QueryDefinition { return SearchTicketsDefinition }

// Invoke reads one page of GET /api/v1/tickets/search, newest modification first.
func (operation SearchTicketsOperation) Invoke(call sdkgo.Call, input SearchTicketsInput) sdkgo.QueryAttempt[SearchTicketsOutput] {
	parameters, err := BuildTicketSearchParameters(input, operation.client.now())
	if err != nil {
		return sdkgo.NewQueryBranch(SearchTicketsBranchDefect, SearchTicketsOutput{}, deskFailurePointer(sdkgo.FailureValidation, searchTicketsOperation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, failure := operation.client.startSession(call, searchTicketsOperation)
	if failure != nil {
		return queryAttemptForSession[SearchTicketsOutput](failure, SearchTicketsBranchProviderRejected, SearchTicketsBranchDefect)
	}
	defer cancel()
	result := operation.client.exchange(session, searchTicketsOperation, deskRequest{method: http.MethodGet, path: "/tickets/search", query: parameters})
	receipt := operation.client.receipt(call, result.response, "")
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRateLimited, exchangeNotSent, exchangeUnavailable:
		return sdkgo.NewQueryRetry[SearchTicketsOutput](result.failure, result.retryAfter)
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(SearchTicketsBranchInvalidResponse, SearchTicketsOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewQueryBranch(SearchTicketsBranchDefect, SearchTicketsOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(SearchTicketsBranchProviderRejected, SearchTicketsOutput{}, &result.failure, receipt)
	}
	output, err := decodeSearchPage(result.response, input)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchTicketsBranchInvalidResponse, SearchTicketsOutput{}, deskFailurePointer(sdkgo.FailureProtocol, searchTicketsOperation, "Zoho Desk returned an invalid search page: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(SearchTicketsBranchSearched, output, nil, receipt)
}

// BuildTicketSearchParameters returns the query parameters searchTickets sends to
// GET /api/v1/tickets/search, such as status=${OPEN},${ONHOLD}&email=jane@example.com&from=0&limit=25.
// now is the upper bound of modifiedTimeRange when ModifiedSince is set. Every value is validated,
// so caller text cannot add a wildcard, an empty check, or another value.
func BuildTicketSearchParameters(input SearchTicketsInput, now time.Time) (url.Values, error) {
	if len(input.Statuses) != 0 && len(input.StatusTypes) != 0 {
		return nil, errors.New("set statuses or statusTypes, not both")
	}
	parameters := url.Values{}
	statusTerms := make([]string, 0, len(input.Statuses)+len(input.StatusTypes))
	for _, status := range input.Statuses {
		if err := validatePicklistValue("statuses entry", string(status)); err != nil {
			return nil, err
		}
		statusTerms = append(statusTerms, string(status))
	}
	for _, statusType := range input.StatusTypes {
		token, isKnown := statusTypeSearchTokens[statusType]
		if !isKnown {
			return nil, fmt.Errorf("statusTypes entry %q must be Open, On Hold, or Closed", statusType)
		}
		statusTerms = append(statusTerms, token)
	}
	if err := setAnyOfParameter(parameters, "status", "statuses", statusTerms); err != nil {
		return nil, err
	}
	priorityTerms := make([]string, 0, len(input.Priorities))
	for _, priority := range input.Priorities {
		if err := validatePicklistValue("priorities entry", string(priority)); err != nil {
			return nil, err
		}
		priorityTerms = append(priorityTerms, string(priority))
	}
	if err := setAnyOfParameter(parameters, "priority", "priorities", priorityTerms); err != nil {
		return nil, err
	}
	if input.ContactEmail != "" {
		if !isBareEmailAddress(input.ContactEmail) {
			return nil, errors.New("contactEmail must be one bare email address such as jane@example.com")
		}
		parameters.Set("email", input.ContactEmail)
	}
	if err := validateOptionalZohoID("departmentId", input.DepartmentID); err != nil {
		return nil, err
	}
	if input.DepartmentID != "" {
		parameters.Set("departmentId", input.DepartmentID)
	}
	if input.ModifiedSince != "" {
		modifiedSince, err := time.Parse(time.RFC3339, input.ModifiedSince)
		if err != nil {
			return nil, errors.New("modifiedSince must be an RFC 3339 instant with an explicit offset, such as 2026-01-21T00:00:00Z")
		}
		if modifiedSince.After(now) {
			return nil, errors.New("modifiedSince cannot be in the future")
		}
		parameters.Set("modifiedTimeRange", formatZohoTimestamp(modifiedSince)+","+formatZohoTimestamp(now))
	}
	if len(parameters) == 0 {
		return nil, errors.New("at least one of statuses, statusTypes, priorities, contactEmail, departmentId, or modifiedSince is required")
	}
	if input.From < 0 || input.From >= MaxSearchResults || input.Limit < 0 || input.Limit > MaxSearchLimit {
		return nil, fmt.Errorf("from must be 0 to %d, and limit 1 to %d or zero for %d", MaxSearchResults-1, MaxSearchLimit, DefaultSearchLimit)
	}
	parameters.Set("from", strconv.Itoa(input.From))
	parameters.Set("limit", strconv.Itoa(effectiveSearchLimit(input)))
	parameters.Set("sortBy", searchSortOrder)
	return parameters, nil
}

// setAnyOfParameter joins distinct values with the comma Zoho Desk reads as "any of".
func setAnyOfParameter(parameters url.Values, name string, inputName string, values []string) error {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		folded := strings.ToLower(value)
		if seen[folded] {
			return fmt.Errorf("%s lists %q twice", inputName, value)
		}
		seen[folded] = true
	}
	if len(values) != 0 {
		parameters.Set(name, strings.Join(values, ","))
	}
	return nil
}

// effectiveSearchLimit shortens the page that would pass result 5000, so a returned NextFrom is always readable.
func effectiveSearchLimit(input SearchTicketsInput) int {
	limit := input.Limit
	if limit == 0 {
		limit = DefaultSearchLimit
	}
	return min(limit, MaxSearchResults-input.From)
}

// decodeSearchPage reads Zoho Desk's {data, count} page; a 204 is an empty page.
func decodeSearchPage(response deskResponse, input SearchTicketsInput) (SearchTicketsOutput, error) {
	output := SearchTicketsOutput{Tickets: []Ticket{}}
	if response.statusCode == http.StatusNoContent {
		return output, nil
	}
	var wire searchPageWire
	if err := json.Unmarshal(response.body, &wire); err != nil {
		return SearchTicketsOutput{}, errors.New("search page is not a JSON object with data and count")
	}
	if wire.Data == nil {
		return SearchTicketsOutput{}, errors.New("search page has no data array")
	}
	limit := effectiveSearchLimit(input)
	if len(*wire.Data) > limit {
		return SearchTicketsOutput{}, errors.New("search page holds more tickets than requested")
	}
	output.TotalMatched = int(wire.Count)
	for index, item := range *wire.Data {
		ticket, err := decodeTicketWire(item)
		if err != nil {
			return SearchTicketsOutput{}, fmt.Errorf("search result %d: %w", index, err)
		}
		if input.ContactEmail != "" && !hasContactEmail(ticket, item.Contact, input.ContactEmail) {
			continue
		}
		ticket.DescriptionHTML, ticket.IsDescriptionTruncated = "", false
		output.Tickets = append(output.Tickets, ticket)
	}
	if len(*wire.Data) == limit && input.From+limit < MaxSearchResults {
		output.NextFrom = input.From + limit
	}
	return output, nil
}

func hasContactEmail(ticket Ticket, contact *deskContactWire, email string) bool {
	if strings.EqualFold(ticket.Email, email) {
		return true
	}
	return contact != nil && strings.EqualFold(contact.Email, email)
}
