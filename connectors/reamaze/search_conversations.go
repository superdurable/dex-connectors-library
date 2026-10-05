// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze

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
	// SearchPageSize is the number of conversations Re:amaze documents per page.
	SearchPageSize = 30
	// MaxSearchPage is the connector's bound on Page; Re:amaze documents no last page.
	MaxSearchPage = 10000

	searchConversationsOperation = "searchConversations"
)

// ConversationFilter is Re:amaze's conversation view, its filter parameter.
type ConversationFilter string

const (
	// ConversationFilterUnarchived is Re:amaze's default view, every conversation that is not archived.
	ConversationFilterUnarchived ConversationFilter = ""
	// ConversationFilterOpen lists only open conversations.
	ConversationFilterOpen ConversationFilter = "open"
	// ConversationFilterUnassigned lists only unassigned conversations.
	ConversationFilterUnassigned ConversationFilter = "unassigned"
	// ConversationFilterArchived lists only archived conversations.
	ConversationFilterArchived ConversationFilter = "archived"
	// ConversationFilterAll lists every conversation, archived or not.
	ConversationFilterAll ConversationFilter = "all"
)

// ConversationSort is Re:amaze's conversation order, its sort parameter.
type ConversationSort string

const (
	// ConversationSortCreated is Re:amaze's default order, by conversation creation.
	ConversationSortCreated ConversationSort = ""
	// ConversationSortUpdated orders by the latest customer update, newest first.
	ConversationSortUpdated ConversationSort = "updated"
	// ConversationSortChanged orders by any update or status change, newest first.
	ConversationSortChanged ConversationSort = "changed"
)

// SearchConversationsInput selects one page of the brand's conversations with Re:amaze's list
// filters, GET /conversations. Different filters narrow each other.
type SearchConversationsInput struct {
	// Filter is Re:amaze's view: blank for every unarchived conversation, open, unassigned,
	// archived, or all.
	Filter ConversationFilter `json:"filter,omitempty"`
	// RequesterEmail keeps conversations relevant to the user with this email, Re:amaze's for
	// parameter; for a customer, the conversations that customer can see, which can include
	// conversations the customer was copied on. Blank keeps every user.
	RequesterEmail string `json:"requesterEmail,omitempty"`
	// Tags keeps conversations matching these tags, sent comma-separated as Re:amaze's tag parameter.
	Tags []string `json:"tags,omitempty"`
	// Channel keeps conversations in the channel with this slug, Re:amaze's category, such as support.
	Channel string `json:"channel,omitempty"`
	// Sort orders the page: blank by creation, updated by the latest customer update, or changed by
	// any update or status change.
	Sort ConversationSort `json:"sort,omitempty"`
	// CustomerMessageSince keeps conversations whose latest customer message is at or after this
	// RFC 3339 instant, Re:amaze's start_date. Blank applies no lower bound.
	CustomerMessageSince string `json:"customerMessageSince,omitempty"`
	// CustomerMessageUntil keeps conversations whose latest customer message is at or before this
	// RFC 3339 instant, Re:amaze's end_date. Blank applies no upper bound.
	CustomerMessageUntil string `json:"customerMessageUntil,omitempty"`
	// Page is 1 to MaxSearchPage; zero reads the first page.
	Page int `json:"page,omitempty"`
}

// SearchConversationsOutput is one page of matching conversations.
type SearchConversationsOutput struct {
	// Conversations lists the page's conversations without FirstMessage.
	Conversations []Conversation `json:"conversations"`
	// TotalCount is Re:amaze's count of every matching conversation.
	TotalCount int `json:"totalCount"`
	// PageCount is Re:amaze's number of pages.
	PageCount int `json:"pageCount"`
	// NextPage is the page to read next, or zero after the last page.
	NextPage int `json:"nextPage,omitempty"`
}

// SearchConversationsOperation is the searchConversations Query.
type SearchConversationsOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (SearchConversationsOperation) Definition() sdkgo.QueryDefinition {
	return SearchConversationsDefinition
}

// Invoke reads one page of GET /conversations with the input's filters.
func (operation SearchConversationsOperation) Invoke(call sdkgo.Call, input SearchConversationsInput) sdkgo.QueryAttempt[SearchConversationsOutput] {
	query, err := BuildConversationSearchQuery(input)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchConversationsBranchDefect, SearchConversationsOutput{}, reamazeFailurePointer(sdkgo.FailureValidation, searchConversationsOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, err := operation.client.resolveCredentials(call)
	if err != nil {
		return credentialQueryAttempt[SearchConversationsOutput](searchConversationsOperation, SearchConversationsBranchDefect, err)
	}
	result := operation.client.exchange(call, credentials, searchConversationsOperation, reamazeRequest{method: http.MethodGet, path: "/conversations", query: query})
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
	output, err := decodeConversationPage(result.response.body, searchPage(input))
	if err != nil {
		return sdkgo.NewQueryBranch(SearchConversationsBranchInvalidResponse, SearchConversationsOutput{}, reamazeFailurePointer(sdkgo.FailureProtocol, searchConversationsOperation, "Re:amaze returned an invalid conversation page: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(SearchConversationsBranchSearched, output, nil, receipt)
}

// BuildConversationSearchQuery validates the input and returns the GET /conversations query parameters.
func BuildConversationSearchQuery(input SearchConversationsInput) (url.Values, error) {
	query := url.Values{"page": {strconv.Itoa(searchPage(input))}}
	switch input.Filter {
	case ConversationFilterUnarchived:
	case ConversationFilterOpen, ConversationFilterUnassigned, ConversationFilterArchived, ConversationFilterAll:
		query.Set("filter", string(input.Filter))
	default:
		return nil, fmt.Errorf("filter %q must be blank, open, unassigned, archived, or all", input.Filter)
	}
	if input.RequesterEmail != "" {
		if !isBareEmailAddress(input.RequesterEmail) {
			return nil, errors.New("requesterEmail must be one bare email address such as jane@example.com")
		}
		query.Set("for", input.RequesterEmail)
	}
	if err := validateTags("tags", input.Tags); err != nil {
		return nil, err
	}
	if len(input.Tags) != 0 {
		query.Set("tag", strings.Join(input.Tags, ","))
	}
	if input.Channel != "" {
		if err := validateChannelSlug("channel", input.Channel); err != nil {
			return nil, err
		}
		query.Set("category", input.Channel)
	}
	switch input.Sort {
	case ConversationSortCreated:
	case ConversationSortUpdated, ConversationSortChanged:
		query.Set("sort", string(input.Sort))
	default:
		return nil, fmt.Errorf("sort %q must be blank, updated, or changed", input.Sort)
	}
	for _, bound := range []struct{ fieldName, parameter, value string }{
		{"customerMessageSince", "start_date", input.CustomerMessageSince},
		{"customerMessageUntil", "end_date", input.CustomerMessageUntil},
	} {
		if bound.value == "" {
			continue
		}
		if _, err := time.Parse(time.RFC3339, bound.value); err != nil {
			return nil, fmt.Errorf("%s must be an RFC 3339 instant such as 2026-01-21T00:00:00Z", bound.fieldName)
		}
		query.Set(bound.parameter, bound.value)
	}
	if input.Page < 0 || input.Page > MaxSearchPage {
		return nil, fmt.Errorf("page must be between 1 and %d, or zero for the first page", MaxSearchPage)
	}
	return query, nil
}

func searchPage(input SearchConversationsInput) int {
	if input.Page == 0 {
		return 1
	}
	return input.Page
}

// decodeConversationPage decodes one GET /conversations page in Re:amaze's order.
func decodeConversationPage(body []byte, page int) (SearchConversationsOutput, error) {
	var document struct {
		pageWire
		Conversations *[]conversationWire `json:"conversations"`
	}
	if err := json.Unmarshal(body, &document); err != nil || document.Conversations == nil {
		return SearchConversationsOutput{}, errors.New("response is not a conversation page")
	}
	if err := document.pageWire.validate(len(*document.Conversations)); err != nil {
		return SearchConversationsOutput{}, err
	}
	output := SearchConversationsOutput{
		Conversations: make([]Conversation, 0, len(*document.Conversations)),
		TotalCount:    *document.TotalCount, PageCount: *document.PageCount,
	}
	for index, wire := range *document.Conversations {
		conversation, err := decodeConversationWire(wire, false)
		if err != nil {
			return SearchConversationsOutput{}, fmt.Errorf("conversation %d: %w", index, err)
		}
		output.Conversations = append(output.Conversations, conversation)
	}
	if page < output.PageCount && page < MaxSearchPage {
		output.NextPage = page + 1
	}
	return output, nil
}
