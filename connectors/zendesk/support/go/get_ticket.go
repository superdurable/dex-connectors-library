// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support

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
	// DefaultLatestCommentLimit is the number of comments getTicket returns when LatestCommentLimit is zero.
	DefaultLatestCommentLimit = 5
	// MaxLatestCommentLimit is the largest accepted LatestCommentLimit.
	MaxLatestCommentLimit = 20

	getTicketOperation = "getTicket"
)

// GetTicketInput names one ticket and how many of its latest comments to return.
type GetTicketInput struct {
	// TicketID is the Zendesk ticket ID.
	TicketID int64 `json:"ticketId"`
	// LatestCommentLimit is 1 to MaxLatestCommentLimit; zero uses DefaultLatestCommentLimit.
	LatestCommentLimit int `json:"latestCommentLimit,omitempty"`
}

// TicketDetails is one ticket with its requester and latest comments.
type TicketDetails struct {
	// Ticket is the ticket, including its description.
	Ticket Ticket `json:"ticket"`
	// Requester is the ticket's requester, or nil when Zendesk did not return the user.
	Requester *TicketUser `json:"requester,omitempty"`
	// LatestComments lists the newest comments first, public replies and internal notes alike.
	LatestComments []TicketComment `json:"latestComments"`
	// HasOlderComments reports that the ticket has comments older than LatestComments.
	HasOlderComments bool `json:"hasOlderComments,omitempty"`
}

// GetTicketOperation is the getTicket Query.
type GetTicketOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (GetTicketOperation) Definition() sdkgo.QueryDefinition { return GetTicketDefinition }

// Invoke reads the ticket with its users sideloaded, then its newest comments.
func (operation GetTicketOperation) Invoke(call sdkgo.Call, input GetTicketInput) sdkgo.QueryAttempt[TicketDetails] {
	if err := validateGetTicketInput(input); err != nil {
		return sdkgo.NewQueryBranch(GetTicketBranchDefect, TicketDetails{}, zendeskFailurePointer(sdkgo.FailureValidation, getTicketOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, getTicketOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetTicketBranchDefect, TicketDetails{}, failure, sdkgo.Receipt{})
	}
	ticketResult := operation.client.exchange(call, credentials, getTicketOperation, zendeskRequest{
		method: http.MethodGet, path: ticketPath(input.TicketID), query: url.Values{"include": {"users"}},
	})
	receipt := operation.client.receipt(call, ticketResult.response, input.TicketID)
	if attempt, isTerminal := getTicketAttemptForExchange(ticketResult, receipt); isTerminal {
		return attempt
	}
	details, err := decodeTicketWithUsers(ticketResult.response.body, input.TicketID, operation.client.agentTicketURL)
	if err != nil {
		return sdkgo.NewQueryBranch(GetTicketBranchInvalidResponse, TicketDetails{}, zendeskFailurePointer(sdkgo.FailureProtocol, getTicketOperation, "Zendesk returned an invalid ticket: "+err.Error()), receipt)
	}
	commentLimit := input.LatestCommentLimit
	if commentLimit == 0 {
		commentLimit = DefaultLatestCommentLimit
	}
	commentResult := operation.client.exchange(call, credentials, getTicketOperation, zendeskRequest{
		method: http.MethodGet, path: ticketPath(input.TicketID) + "/comments",
		query: url.Values{"sort": {"-created_at"}, "page[size]": {strconv.Itoa(commentLimit)}},
	})
	receipt = operation.client.receipt(call, commentResult.response, input.TicketID)
	if attempt, isTerminal := getTicketAttemptForExchange(commentResult, receipt); isTerminal {
		return attempt
	}
	comments, hasOlderComments, err := decodeLatestComments(commentResult.response.body, commentLimit)
	if err != nil {
		return sdkgo.NewQueryBranch(GetTicketBranchInvalidResponse, TicketDetails{}, zendeskFailurePointer(sdkgo.FailureProtocol, getTicketOperation, "Zendesk returned an invalid comment page: "+err.Error()), receipt)
	}
	details.LatestComments, details.HasOlderComments = comments, hasOlderComments
	return sdkgo.NewQueryBranch(GetTicketBranchFound, details, nil, receipt)
}

// getTicketAttemptForExchange returns the terminal attempt for every outcome except success.
func getTicketAttemptForExchange(result zendeskExchange, receipt sdkgo.Receipt) (sdkgo.QueryAttempt[TicketDetails], bool) {
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.QueryAttempt[TicketDetails]{}, false
	case exchangeRetryable:
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
		return errors.New("ticketId must be a positive Zendesk ticket ID")
	}
	if input.LatestCommentLimit < 0 || input.LatestCommentLimit > MaxLatestCommentLimit {
		return fmt.Errorf("latestCommentLimit must be between 1 and %d, or zero for %d", MaxLatestCommentLimit, DefaultLatestCommentLimit)
	}
	return nil
}

func decodeTicketWithUsers(body []byte, ticketID int64, agentTicketURL string) (TicketDetails, error) {
	var envelope struct {
		Ticket *zendeskTicketWire `json:"ticket"`
		Users  []zendeskUserWire  `json:"users"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return TicketDetails{}, errors.New("ticket response is not JSON")
	}
	if envelope.Ticket == nil {
		return TicketDetails{}, errors.New("ticket response has no ticket")
	}
	decoded, err := decodeTicketWire(*envelope.Ticket, agentTicketURL)
	if err != nil {
		return TicketDetails{}, err
	}
	if decoded.ticket.ID != ticketID {
		return TicketDetails{}, errors.New("ticket response is for another ticket")
	}
	details := TicketDetails{Ticket: decoded.ticket, LatestComments: []TicketComment{}}
	for _, user := range envelope.Users {
		if user.ID == decoded.ticket.RequesterID && user.ID > 0 {
			requester := decodeUserWire(user)
			details.Requester = &requester
			break
		}
	}
	return details, nil
}

// decodeLatestComments orders the page newest first, even if Zendesk returned it ascending.
func decodeLatestComments(body []byte, commentLimit int) ([]TicketComment, bool, error) {
	var page struct {
		Comments []zendeskCommentWire `json:"comments"`
		Meta     struct {
			HasMore bool `json:"has_more"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, false, errors.New("comment page is not JSON")
	}
	if page.Comments == nil {
		return nil, false, errors.New("comment page has no comments array")
	}
	if len(page.Comments) > commentLimit {
		return nil, false, errors.New("comment page is larger than requested")
	}
	comments := make([]TicketComment, 0, len(page.Comments))
	for index, wire := range page.Comments {
		comment, err := decodeCommentWire(wire)
		if err != nil {
			return nil, false, fmt.Errorf("comment %d: %w", index, err)
		}
		comments = append(comments, comment)
	}
	sort.SliceStable(comments, func(left, right int) bool {
		if !comments[left].CreatedAt.Equal(comments[right].CreatedAt) {
			return comments[left].CreatedAt.After(comments[right].CreatedAt)
		}
		return comments[left].ID > comments[right].ID
	})
	return comments, page.Meta.HasMore, nil
}
