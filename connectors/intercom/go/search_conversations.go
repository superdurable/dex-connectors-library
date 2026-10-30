// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// DefaultSearchPageSize is the page size used when SearchConversationsInput.PageSize is zero; it is Intercom's default.
	DefaultSearchPageSize = 20
	// MaxSearchPageSize is the largest accepted page size, Intercom's documented maximum.
	MaxSearchPageSize = 150
	// MaxSearchFilterValues bounds each multi-valued filter; Intercom allows 15 filters in one AND or OR group.
	MaxSearchFilterValues = 15

	maximumCursorLength          = 2048
	searchConversationsOperation = "searchConversations"
)

// SearchConversationsInput selects conversations with typed filters that the connector turns into one
// Intercom search query. Different filters narrow each other; several states, contact IDs, or tag IDs
// match any of them. At least one filter is required.
type SearchConversationsInput struct {
	// States matches conversations in any of these Intercom states. Empty matches every state.
	States []ConversationState `json:"states,omitempty"`
	// ContactEmail matches conversations whose first message came from this email address, Intercom's
	// source.author.email, such as jane@example.com. An admin-initiated conversation's source author is
	// the admin, so use ContactIDs to find every conversation with a contact.
	ContactEmail string `json:"contactEmail,omitempty"`
	// ContactIDs matches conversations with any of up to 15 Intercom contact IDs, as findContactByEmail returns.
	ContactIDs []string `json:"contactIds,omitempty"`
	// TagIDs matches conversations carrying any of up to 15 Intercom tag IDs; Intercom searches tags by ID, not name.
	TagIDs []string `json:"tagIds,omitempty"`
	// UpdatedSince matches conversations updated at or after this RFC 3339 instant with an explicit
	// offset, such as 2026-01-21T00:00:00Z. Blank applies no time filter.
	UpdatedSince string `json:"updatedSince,omitempty"`
	// PageSize is 1 to MaxSearchPageSize conversations; zero uses DefaultSearchPageSize.
	PageSize int `json:"pageSize,omitempty"`
	// Cursor is a previous page's NextCursor.
	Cursor string `json:"cursor,omitempty"`
}

// SearchConversationsOutput is one page of matching conversations. Intercom's search index can lag a
// change by a short time, so a page can omit a change made moments earlier.
type SearchConversationsOutput struct {
	// Conversations lists the matching conversations without their parts or first-message body.
	Conversations []Conversation `json:"conversations"`
	// TotalCount is Intercom's count of every matching conversation.
	TotalCount int `json:"totalCount"`
	// NextCursor reads the next page; it is empty on the last page.
	NextCursor string `json:"nextCursor,omitempty"`
}

// SearchConversationsOperation is the searchConversations Query.
type SearchConversationsOperation struct {
	client *Client
}

// searchFilterWire is one Intercom search filter, or an AND or OR group whose Value lists filters.
type searchFilterWire struct {
	Field    string `json:"field,omitempty"`
	Operator string `json:"operator"`
	Value    any    `json:"value"`
}

type searchPaginationWire struct {
	PerPage       int    `json:"per_page"`
	StartingAfter string `json:"starting_after,omitempty"`
}

type searchRequestWire struct {
	Query      searchFilterWire     `json:"query"`
	Pagination searchPaginationWire `json:"pagination"`
}

type searchPagesWire struct {
	Next *struct {
		StartingAfter string `json:"starting_after"`
	} `json:"next"`
}

// Definition returns the immutable connector operation definition.
func (SearchConversationsOperation) Definition() sdkgo.QueryDefinition {
	return SearchConversationsDefinition
}

// Invoke reads one page from POST /conversations/search.
func (operation SearchConversationsOperation) Invoke(call sdkgo.Call, input SearchConversationsInput) sdkgo.QueryAttempt[SearchConversationsOutput] {
	query, err := buildConversationSearchFilter(input)
	if err == nil {
		err = validateSearchPage(input.PageSize, input.Cursor)
	}
	if err != nil {
		return sdkgo.NewQueryBranch(SearchConversationsBranchDefect, SearchConversationsOutput{}, intercomFailurePointer(sdkgo.FailureValidation, searchConversationsOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, searchConversationsOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(SearchConversationsBranchDefect, SearchConversationsOutput{}, failure, sdkgo.Receipt{})
	}
	pageSize := searchPageSize(input.PageSize)
	result := operation.client.exchange(call, credentials, searchConversationsOperation, intercomRequest{
		method: http.MethodPost, path: "/conversations/search",
		payload: searchRequestWire{Query: query, Pagination: searchPaginationWire{PerPage: pageSize, StartingAfter: input.Cursor}},
	})
	receipt := operation.client.receipt(call, result.response, "")
	switch {
	case result.outcome == exchangeSucceeded:
	case result.isRetryableRead():
		return sdkgo.NewQueryRetry[SearchConversationsOutput](result.failure, result.retryAfter)
	case result.outcome == exchangeInvalid:
		return sdkgo.NewQueryBranch(SearchConversationsBranchInvalidResponse, SearchConversationsOutput{}, &result.failure, receipt)
	case result.outcome == exchangeDefect:
		return sdkgo.NewQueryBranch(SearchConversationsBranchDefect, SearchConversationsOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(SearchConversationsBranchProviderRejected, SearchConversationsOutput{}, &result.failure, receipt)
	}
	output, err := decodeConversationSearchPage(result.response.body, pageSize)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchConversationsBranchInvalidResponse, SearchConversationsOutput{}, intercomFailurePointer(sdkgo.FailureProtocol, searchConversationsOperation, "Intercom returned an invalid search page: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(SearchConversationsBranchSearched, output, nil, receipt)
}

// BuildConversationSearchQuery returns the Intercom search query searchConversations sends, such as
// {"operator":"AND","value":[{"field":"state","operator":"=","value":"open"},{"field":"updated_at","operator":">","value":1768953600}]}.
// Several values of one filter become an OR group of = filters.
func BuildConversationSearchQuery(input SearchConversationsInput) (json.RawMessage, error) {
	query, err := buildConversationSearchFilter(input)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(query)
	if err != nil {
		return nil, errors.New("search query could not be encoded")
	}
	return encoded, nil
}

func buildConversationSearchFilter(input SearchConversationsInput) (searchFilterWire, error) {
	var filters []searchFilterWire
	states := make([]string, 0, len(input.States))
	for _, state := range input.States {
		if err := validateConversationState(state); err != nil {
			return searchFilterWire{}, err
		}
		states = append(states, string(state))
	}
	stateFilter, err := anyOfFilter("states", "state", states)
	if err != nil {
		return searchFilterWire{}, err
	}
	filters = append(filters, stateFilter...)
	if input.ContactEmail != "" {
		if !isBareEmailAddress(input.ContactEmail) {
			return searchFilterWire{}, errors.New("contactEmail must be one bare email address such as jane@example.com")
		}
		filters = append(filters, searchFilterWire{Field: "source.author.email", Operator: "=", Value: input.ContactEmail})
	}
	for _, contactID := range input.ContactIDs {
		if !contactIDPattern.MatchString(contactID) {
			return searchFilterWire{}, fmt.Errorf("contactIds entry %q is not an Intercom contact ID", contactID)
		}
	}
	contactFilter, err := anyOfFilter("contactIds", "contact_ids", input.ContactIDs)
	if err != nil {
		return searchFilterWire{}, err
	}
	filters = append(filters, contactFilter...)
	for _, tagID := range input.TagIDs {
		if !tagIDPattern.MatchString(tagID) {
			return searchFilterWire{}, fmt.Errorf("tagIds entry %q is not an Intercom tag ID of digits", tagID)
		}
	}
	tagFilter, err := anyOfFilter("tagIds", "tag_ids", input.TagIDs)
	if err != nil {
		return searchFilterWire{}, err
	}
	filters = append(filters, tagFilter...)
	if input.UpdatedSince != "" {
		updatedSince, err := time.Parse(time.RFC3339, input.UpdatedSince)
		if err != nil || updatedSince.Unix() < 0 {
			return searchFilterWire{}, errors.New("updatedSince must be an RFC 3339 instant with an explicit offset, such as 2026-01-21T00:00:00Z")
		}
		// Intercom documents > on a conversation timestamp as greater than or equal.
		filters = append(filters, searchFilterWire{Field: "updated_at", Operator: ">", Value: updatedSince.Unix()})
	}
	switch len(filters) {
	case 0:
		return searchFilterWire{}, errors.New("at least one of states, contactEmail, contactIds, tagIds, or updatedSince is required")
	case 1:
		return filters[0], nil
	default:
		return searchFilterWire{Operator: "AND", Value: filters}, nil
	}
}

// anyOfFilter returns nothing, one = filter, or an OR group of = filters for one field.
func anyOfFilter(inputName string, field string, values []string) ([]searchFilterWire, error) {
	if len(values) > MaxSearchFilterValues {
		return nil, fmt.Errorf("%s has more than %d values", inputName, MaxSearchFilterValues)
	}
	seen := make(map[string]bool, len(values))
	filters := make([]searchFilterWire, 0, len(values))
	for _, value := range values {
		if seen[value] {
			return nil, fmt.Errorf("%s lists %q twice", inputName, value)
		}
		seen[value] = true
		filters = append(filters, searchFilterWire{Field: field, Operator: "=", Value: value})
	}
	switch len(filters) {
	case 0:
		return nil, nil
	case 1:
		return filters, nil
	default:
		return []searchFilterWire{{Operator: "OR", Value: filters}}, nil
	}
}

func validateSearchPage(pageSize int, cursor string) error {
	if pageSize < 0 || pageSize > MaxSearchPageSize {
		return fmt.Errorf("pageSize must be between 1 and %d, or zero for %d", MaxSearchPageSize, DefaultSearchPageSize)
	}
	if len(cursor) > maximumCursorLength {
		return errors.New("cursor is longer than any Intercom cursor")
	}
	for index := 0; index < len(cursor); index++ {
		if cursor[index] <= ' ' || cursor[index] > '~' {
			return errors.New("cursor must be the printable NextCursor of a previous page")
		}
	}
	return nil
}

func searchPageSize(pageSize int) int {
	if pageSize == 0 {
		return DefaultSearchPageSize
	}
	return pageSize
}

func decodeConversationSearchPage(body []byte, pageSize int) (SearchConversationsOutput, error) {
	var page struct {
		Type          string                     `json:"type"`
		Conversations []intercomConversationWire `json:"conversations"`
		TotalCount    int                        `json:"total_count"`
		Pages         *searchPagesWire           `json:"pages"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return SearchConversationsOutput{}, errors.New("search page is not a conversation list")
	}
	if page.Type != "" && page.Type != "conversation.list" {
		return SearchConversationsOutput{}, errors.New("search page is not a conversation list")
	}
	if page.Conversations == nil {
		return SearchConversationsOutput{}, errors.New("search page has no conversations array")
	}
	if len(page.Conversations) > pageSize {
		return SearchConversationsOutput{}, errors.New("search page is larger than requested")
	}
	output := SearchConversationsOutput{Conversations: make([]Conversation, 0, len(page.Conversations)), TotalCount: page.TotalCount}
	for index, wire := range page.Conversations {
		conversation, err := decodeConversationWire(wire, false)
		if err != nil {
			return SearchConversationsOutput{}, fmt.Errorf("conversation %d: %w", index, err)
		}
		output.Conversations = append(output.Conversations, conversation)
	}
	if page.Pages != nil && page.Pages.Next != nil {
		if err := validateSearchPage(0, page.Pages.Next.StartingAfter); err != nil {
			return SearchConversationsOutput{}, errors.New("search page cursor is not printable")
		}
		output.NextCursor = page.Pages.Next.StartingAfter
	}
	return output, nil
}
