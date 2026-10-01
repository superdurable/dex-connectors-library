// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// DefaultConversationLimit is the number of conversations getTicket returns when ConversationLimit is zero.
	DefaultConversationLimit = 10
	// MaxConversationLimit is the largest accepted ConversationLimit.
	MaxConversationLimit = 20

	getTicketOperation = "getTicket"
)

// GetTicketInput names one ticket and how many of its first conversations to return.
type GetTicketInput struct {
	// TicketID is the Freshdesk ticket ID.
	TicketID int64 `json:"ticketId"`
	// ConversationLimit is 1 to MaxConversationLimit; zero uses DefaultConversationLimit.
	ConversationLimit int `json:"conversationLimit,omitempty"`
}

// TicketDetails is one ticket with its requester and first conversations.
type TicketDetails struct {
	// Ticket is the ticket, including its plain-text description and tags.
	Ticket Ticket `json:"ticket"`
	// Requester is the ticket's requester, or nil when Freshdesk did not embed one.
	Requester *TicketRequester `json:"requester,omitempty"`
	// Conversations lists the ticket's replies and notes oldest first, as Freshdesk orders them.
	Conversations []TicketConversation `json:"conversations"`
	// HasMoreConversations reports that the ticket has conversations after the last one returned.
	HasMoreConversations bool `json:"hasMoreConversations,omitempty"`
}

// GetTicketOperation is the getTicket Query.
type GetTicketOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (GetTicketOperation) Definition() sdkgo.QueryDefinition { return GetTicketDefinition }

// Invoke reads the ticket with its requester embedded, then the first page of its conversations.
func (operation GetTicketOperation) Invoke(call sdkgo.Call, input GetTicketInput) sdkgo.QueryAttempt[TicketDetails] {
	if err := validateGetTicketInput(input); err != nil {
		return sdkgo.NewQueryBranch(GetTicketBranchDefect, TicketDetails{}, freshdeskFailurePointer(sdkgo.FailureValidation, getTicketOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, getTicketOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetTicketBranchDefect, TicketDetails{}, failure, sdkgo.Receipt{})
	}
	ticketResult := operation.client.exchange(call, credentials, getTicketOperation, freshdeskRequest{
		method: http.MethodGet, path: ticketPath(input.TicketID), query: url.Values{"include": {"requester"}},
	})
	receipt := operation.client.receipt(call, ticketResult.response, input.TicketID)
	if attempt, isTerminal := getTicketAttemptForExchange(ticketResult, receipt); isTerminal {
		return attempt
	}
	details, err := decodeTicketWithRequester(ticketResult.response.body, input.TicketID)
	if err != nil {
		return sdkgo.NewQueryBranch(GetTicketBranchInvalidResponse, TicketDetails{}, freshdeskFailurePointer(sdkgo.FailureProtocol, getTicketOperation, "Freshdesk returned an invalid ticket: "+err.Error()), receipt)
	}
	conversationLimit := input.ConversationLimit
	if conversationLimit == 0 {
		conversationLimit = DefaultConversationLimit
	}
	conversationResult := operation.client.exchange(call, credentials, getTicketOperation, freshdeskRequest{
		method: http.MethodGet, path: ticketPath(input.TicketID) + "/conversations",
		query: url.Values{"per_page": {strconv.Itoa(conversationLimit)}, "page": {"1"}},
	})
	receipt = operation.client.receipt(call, conversationResult.response, input.TicketID)
	if attempt, isTerminal := getTicketAttemptForExchange(conversationResult, receipt); isTerminal {
		return attempt
	}
	conversations, err := decodeConversationPage(conversationResult.response.body, conversationLimit)
	if err != nil {
		return sdkgo.NewQueryBranch(GetTicketBranchInvalidResponse, TicketDetails{}, freshdeskFailurePointer(sdkgo.FailureProtocol, getTicketOperation, "Freshdesk returned an invalid conversation page: "+err.Error()), receipt)
	}
	details.Conversations, details.HasMoreConversations = conversations, hasNextPage(conversationResult.response.header)
	return sdkgo.NewQueryBranch(GetTicketBranchFound, details, nil, receipt)
}

// getTicketAttemptForExchange returns the terminal attempt for every outcome except success.
func getTicketAttemptForExchange(result freshdeskExchange, receipt sdkgo.Receipt) (sdkgo.QueryAttempt[TicketDetails], bool) {
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.QueryAttempt[TicketDetails]{}, false
	case exchangeRateLimited, exchangeNotSent, exchangeUnavailable:
		return sdkgo.NewQueryRetry[TicketDetails](result.failure, result.retryAfter), true
	case exchangeNotFound:
		return sdkgo.NewQueryBranch(GetTicketBranchNotFound, TicketDetails{}, &result.failure, receipt), true
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(GetTicketBranchInvalidResponse, TicketDetails{}, &result.failure, receipt), true
	case exchangeDefect:
		return sdkgo.NewQueryBranch(GetTicketBranchDefect, TicketDetails{}, &result.failure, receipt), true
	default:
		return sdkgo.NewQueryBranch(GetTicketBranchProviderRejected, TicketDetails{}, &result.failure, receipt), true
	}
}

func validateGetTicketInput(input GetTicketInput) error {
	if input.TicketID < 1 {
		return errors.New("ticketId must be a positive Freshdesk ticket ID")
	}
	if input.ConversationLimit < 0 || input.ConversationLimit > MaxConversationLimit {
		return fmt.Errorf("conversationLimit must be between 1 and %d, or zero for %d", MaxConversationLimit, DefaultConversationLimit)
	}
	return nil
}

func decodeTicketWithRequester(body []byte, ticketID int64) (TicketDetails, error) {
	ticket, err := decodeTicketBody(body, ticketID)
	if err != nil {
		return TicketDetails{}, err
	}
	var embedded struct {
		Requester *freshdeskRequesterWire `json:"requester"`
	}
	if err := json.Unmarshal(body, &embedded); err != nil {
		return TicketDetails{}, errors.New("ticket response is not JSON")
	}
	details := TicketDetails{Ticket: ticket, Conversations: []TicketConversation{}}
	if embedded.Requester != nil && embedded.Requester.ID > 0 {
		if embedded.Requester.ID != ticket.RequesterID {
			return TicketDetails{}, errors.New("embedded requester is not the ticket's requester")
		}
		details.Requester = &TicketRequester{ID: embedded.Requester.ID, Name: embedded.Requester.Name, Email: embedded.Requester.Email}
	}
	return details, nil
}

// decodeConversationPage orders the page oldest first, even if Freshdesk returned another order.
func decodeConversationPage(body []byte, conversationLimit int) ([]TicketConversation, error) {
	var wires []freshdeskConversationWire
	if err := json.Unmarshal(body, &wires); err != nil {
		return nil, errors.New("conversation page is not a JSON array of conversations")
	}
	if len(wires) > conversationLimit {
		return nil, errors.New("conversation page is larger than requested")
	}
	conversations := make([]TicketConversation, 0, len(wires))
	for index, wire := range wires {
		conversation, err := decodeConversationWire(wire)
		if err != nil {
			return nil, fmt.Errorf("conversation %d: %w", index, err)
		}
		conversations = append(conversations, conversation)
	}
	sort.SliceStable(conversations, func(left, right int) bool {
		if !conversations[left].CreatedAt.Equal(conversations[right].CreatedAt) {
			return conversations[left].CreatedAt.Before(conversations[right].CreatedAt)
		}
		return conversations[left].ID < conversations[right].ID
	})
	return conversations, nil
}
