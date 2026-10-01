// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout

import (
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
	// ConversationsPageSize is Help Scout's fixed page size for listing conversations.
	ConversationsPageSize = 25
	// MaxSearchPage is the highest page number searchConversations accepts.
	MaxSearchPage = 10000

	// maxConversationsPerPage rejects a page far larger than Help Scout documents.
	maxConversationsPerPage = 100
)

// SearchConversationsInput filters Help Scout conversations. Filters narrow each other, and a blank or
// zero filter applies no restriction, so an empty input lists active conversations in every inbox the
// app's Help Scout user can access.
type SearchConversationsInput struct {
	// MailboxID limits the search to one inbox, such as the mailboxPicker unit's mailboxId; zero searches
	// every inbox.
	MailboxID int64 `json:"mailboxId,omitempty"`
	// Status is active, pending, closed, spam, or all. Blank lists active conversations, Help Scout's default.
	Status ConversationStatus `json:"status,omitempty"`
	// Tag limits the search to conversations carrying this tag name.
	Tag string `json:"tag,omitempty"`
	// CustomerEmail matches conversations where the address is in To, Cc, or Bcc or is one of a customer
	// profile's emails, as Help Scout's email search documents, such as jane@example.com.
	CustomerEmail string `json:"customerEmail,omitempty"`
	// ModifiedSince matches conversations modified after this RFC 3339 instant with an explicit offset,
	// such as 2026-01-21T00:00:00Z. Help Scout compares whole seconds. Blank applies no time filter.
	ModifiedSince string `json:"modifiedSince,omitempty"`
	// Page is the 1-based page to read, at most MaxSearchPage; zero reads page 1. Pass a previous
	// result's NextPage to continue.
	Page int `json:"page,omitempty"`
}

// SearchConversationsOutput is one page of at most ConversationsPageSize matching conversations, newest
// created first, without threads.
type SearchConversationsOutput struct {
	// Conversations lists the page's conversations.
	Conversations []Conversation `json:"conversations"`
	// Page is the 1-based page that was read.
	Page int `json:"page"`
	// NextPage is the page to pass for more results, or zero on the last page.
	NextPage int `json:"nextPage,omitempty"`
	// TotalPages is Help Scout's page count for the search.
	TotalPages int `json:"totalPages"`
	// TotalConversations is Help Scout's count of matching conversations.
	TotalConversations int64 `json:"totalConversations"`
}

// SearchConversationsOperation implements the searchConversations Query. Build it with
// Client.SearchConversations.
type SearchConversationsOperation struct{ client *Client }

type conversationPageWire struct {
	Embedded struct {
		Conversations []helpScoutConversationWire `json:"conversations"`
	} `json:"_embedded"`
	Links helpScoutPageLinks `json:"_links"`
	Page  struct {
		TotalElements int64 `json:"totalElements"`
		TotalPages    int   `json:"totalPages"`
	} `json:"page"`
}

// Definition returns the immutable searchConversations operation definition.
func (SearchConversationsOperation) Definition() sdkgo.QueryDefinition {
	return SearchConversationsDefinition
}

// Invoke reads one page of GET /v2/conversations with the typed filters as URL parameters; the customer
// email becomes Help Scout's query=(email:"...") search, which Help Scout combines with the others by AND.
func (operation SearchConversationsOperation) Invoke(call sdkgo.Call, input SearchConversationsInput) sdkgo.QueryAttempt[SearchConversationsOutput] {
	operationID := SearchConversationsDefinition.Operation.OperationID
	parameters, err := BuildConversationSearchParameters(input)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchConversationsBranchDefect, SearchConversationsOutput{},
			helpScoutFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	credentials, err := operation.client.resolveCredentials(call)
	if err != nil {
		return credentialQueryAttempt[SearchConversationsOutput](operationID, SearchConversationsBranchDefect, err)
	}
	response, err := operation.client.send(call.Context, call, &credentials,
		helpScoutRequest{method: http.MethodGet, path: "/v2/conversations", query: parameters}, nil)
	branches := failureBranches{providerRejected: SearchConversationsBranchProviderRejected, invalidResponse: SearchConversationsBranchInvalidResponse}
	if attempt, isTerminal := classifyQueryExchange[SearchConversationsOutput](operation.client, call, operationID, credentials, response, err, branches, 0); isTerminal {
		return attempt
	}
	output, err := decodeConversationPage(response.body, searchPage(input))
	if err != nil {
		return sdkgo.NewQueryBranch(SearchConversationsBranchInvalidResponse, SearchConversationsOutput{},
			helpScoutFailurePointer(operationID, sdkgo.FailureProtocol, "Help Scout returned an invalid conversation page: "+err.Error()),
			operation.client.receipt(call, 0, ""))
	}
	return sdkgo.NewQueryBranch(SearchConversationsBranchSearched, output, nil, operation.client.receipt(call, 0, ""))
}

// BuildConversationSearchParameters returns the GET /v2/conversations URL parameters searchConversations
// sends, such as mailbox=123&status=pending&tag=billing&query=(email:"jane@example.com")&page=1, so a
// Flow author can review the request a filter produces.
func BuildConversationSearchParameters(input SearchConversationsInput) (url.Values, error) {
	if input.Page < 0 || input.Page > MaxSearchPage {
		return nil, fmt.Errorf("page must be from 1 through %d, or zero for page 1", MaxSearchPage)
	}
	parameters := url.Values{"page": {strconv.Itoa(searchPage(input))}}
	if input.MailboxID < 0 {
		return nil, errors.New("mailboxId must be a positive Help Scout inbox ID, or zero for every inbox")
	}
	if input.MailboxID > 0 {
		parameters.Set("mailbox", strconv.FormatInt(input.MailboxID, 10))
	}
	if input.Status != "" {
		if input.Status != ConversationStatusAll {
			if err := validateConversationStatus("status", input.Status); err != nil {
				return nil, fmt.Errorf("%w, or all", err)
			}
		}
		parameters.Set("status", string(input.Status))
	}
	if input.Tag != "" {
		if err := validateTags("tag", []string{input.Tag}); err != nil {
			return nil, err
		}
		parameters.Set("tag", input.Tag)
	}
	if input.CustomerEmail != "" {
		if !isBareEmailAddress(input.CustomerEmail) {
			return nil, errors.New("customerEmail must be one bare email address such as jane@example.com")
		}
		parameters.Set("query", `(email:"`+input.CustomerEmail+`")`)
	}
	if input.ModifiedSince != "" {
		modifiedSince, err := time.Parse(time.RFC3339, input.ModifiedSince)
		if err != nil {
			return nil, errors.New("modifiedSince must be an RFC 3339 instant with an explicit offset, such as 2026-01-21T00:00:00Z")
		}
		parameters.Set("modifiedSince", formatHelpScoutTimestamp(modifiedSince))
	}
	return parameters, nil
}

func searchPage(input SearchConversationsInput) int {
	if input.Page == 0 {
		return 1
	}
	return input.Page
}

func decodeConversationPage(body []byte, page int) (SearchConversationsOutput, error) {
	var wire conversationPageWire
	if err := decodeHelpScoutJSON(body, &wire); err != nil {
		return SearchConversationsOutput{}, errors.New("page is not a JSON object")
	}
	if len(wire.Embedded.Conversations) > maxConversationsPerPage {
		return SearchConversationsOutput{}, errors.New("page holds more conversations than Help Scout's page size")
	}
	output := SearchConversationsOutput{
		Conversations: make([]Conversation, 0, len(wire.Embedded.Conversations)), Page: page,
		TotalPages: wire.Page.TotalPages, TotalConversations: wire.Page.TotalElements,
	}
	for index, conversationWire := range wire.Embedded.Conversations {
		conversation, err := decodeConversation(conversationWire)
		if err != nil {
			return SearchConversationsOutput{}, fmt.Errorf("conversation %d: %w", index, err)
		}
		output.Conversations = append(output.Conversations, conversation)
	}
	if wire.Links.Next != nil && page < MaxSearchPage {
		output.NextPage = page + 1
	}
	return output, nil
}

// isBareEmailAddress rejects display names and the quote, parenthesis, and backslash that would change a
// Help Scout search query.
func isBareEmailAddress(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Name == "" && address.Address == value && !strings.ContainsAny(value, " \"'()<>,;\\")
}
