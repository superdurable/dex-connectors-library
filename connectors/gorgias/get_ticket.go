// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias

import (
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// DefaultLatestMessageLimit is the number of messages getTicket returns when LatestMessageLimit is zero.
	DefaultLatestMessageLimit = 5
	// MaxLatestMessageLimit is the largest accepted LatestMessageLimit.
	MaxLatestMessageLimit = 20

	getTicketOperation = "getTicket"
)

// GetTicketInput names one ticket and how many of its latest messages to return.
type GetTicketInput struct {
	// TicketID is the Gorgias ticket ID.
	TicketID int64 `json:"ticketId"`
	// LatestMessageLimit is 1 to MaxLatestMessageLimit; zero uses DefaultLatestMessageLimit.
	LatestMessageLimit int `json:"latestMessageLimit,omitempty"`
}

// TicketDetails is one ticket with its customer and latest messages.
type TicketDetails struct {
	// Ticket is the ticket, including its tags.
	Ticket Ticket `json:"ticket"`
	// Requester is the ticket's customer, or nil when Gorgias did not embed one.
	Requester *Customer `json:"requester,omitempty"`
	// LatestMessages lists the newest messages first: customer messages, replies, and internal notes alike.
	LatestMessages []TicketMessage `json:"latestMessages"`
	// HasOlderMessages reports that the ticket has messages older than LatestMessages.
	HasOlderMessages bool `json:"hasOlderMessages,omitempty"`
}

// GetTicketOperation is the getTicket Query.
type GetTicketOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (GetTicketOperation) Definition() sdkgo.QueryDefinition { return GetTicketDefinition }

// Invoke reads GET /api/tickets/{id}, which embeds the customer and every message, and keeps the latest messages.
func (operation GetTicketOperation) Invoke(call sdkgo.Call, input GetTicketInput) sdkgo.QueryAttempt[TicketDetails] {
	if err := validateGetTicketInput(input); err != nil {
		return sdkgo.NewQueryBranch(GetTicketBranchDefect, TicketDetails{}, gorgiasFailurePointer(sdkgo.FailureValidation, getTicketOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, getTicketOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetTicketBranchDefect, TicketDetails{}, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call, credentials, getTicketOperation, gorgiasRequest{method: http.MethodGet, path: ticketPath(input.TicketID)})
	receipt := operation.client.receipt(call, result.response, input.TicketID)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRateLimited, exchangeNotSent, exchangeUnavailable:
		return sdkgo.NewQueryRetry[TicketDetails](result.failure, result.retryAfter)
	case exchangeNotFound:
		return sdkgo.NewQueryBranch(GetTicketBranchNotFound, TicketDetails{}, &result.failure, receipt)
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(GetTicketBranchInvalidResponse, TicketDetails{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewQueryBranch(GetTicketBranchDefect, TicketDetails{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(GetTicketBranchProviderRejected, TicketDetails{}, &result.failure, receipt)
	}
	details, err := decodeTicketDetails(result.response.body, input)
	if err != nil {
		return sdkgo.NewQueryBranch(GetTicketBranchInvalidResponse, TicketDetails{}, gorgiasFailurePointer(sdkgo.FailureProtocol, getTicketOperation, "Gorgias returned an invalid ticket: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(GetTicketBranchFound, details, nil, receipt)
}

func validateGetTicketInput(input GetTicketInput) error {
	if input.TicketID < 1 {
		return errors.New("ticketId must be a positive Gorgias ticket ID")
	}
	if input.LatestMessageLimit < 0 || input.LatestMessageLimit > MaxLatestMessageLimit {
		return fmt.Errorf("latestMessageLimit must be between 1 and %d, or zero for %d", MaxLatestMessageLimit, DefaultLatestMessageLimit)
	}
	return nil
}

// decodeTicketDetails orders messages newest first, whatever order Gorgias embedded them in.
func decodeTicketDetails(body []byte, input GetTicketInput) (TicketDetails, error) {
	ticket, ticketWire, err := decodeTicketBody(body, input.TicketID)
	if err != nil {
		return TicketDetails{}, err
	}
	details := TicketDetails{Ticket: ticket, LatestMessages: []TicketMessage{}}
	if ticketWire.Customer != nil {
		requester, err := decodeCustomerWire(*ticketWire.Customer)
		if err != nil {
			return TicketDetails{}, err
		}
		details.Requester = &requester
	}
	messages := make([]TicketMessage, 0, len(ticketWire.Messages))
	for index, wire := range ticketWire.Messages {
		message, err := decodeMessageWire(wire)
		if err != nil {
			return TicketDetails{}, fmt.Errorf("message %d: %w", index, err)
		}
		if message.TicketID != 0 && message.TicketID != ticket.ID {
			return TicketDetails{}, fmt.Errorf("message %d belongs to another ticket", index)
		}
		message.TicketID = ticket.ID
		messages = append(messages, message)
	}
	sort.SliceStable(messages, func(left, right int) bool {
		if !messages[left].CreatedAt.Equal(messages[right].CreatedAt) {
			return messages[left].CreatedAt.After(messages[right].CreatedAt)
		}
		return messages[left].ID > messages[right].ID
	})
	limit := input.LatestMessageLimit
	if limit == 0 {
		limit = DefaultLatestMessageLimit
	}
	details.LatestMessages = messages[:min(limit, len(messages))]
	details.HasOlderMessages = len(messages) > limit
	return details, nil
}
