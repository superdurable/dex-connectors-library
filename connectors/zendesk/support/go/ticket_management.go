// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// ListTicketsInput selects one cursor page of tickets visible to the connection's agent.
type ListTicketsInput struct {
	// PageSize is 1 through 100; zero uses 25.
	PageSize int `json:"pageSize,omitempty"`
	// Cursor is the NextCursor from a previous page; blank starts at the first page.
	Cursor string `json:"cursor,omitempty"`
}

// ListTicketsOutput is one cursor page of tickets.
type ListTicketsOutput struct {
	// Tickets are tickets visible on this page.
	Tickets []Ticket `json:"tickets"`
	// NextCursor is blank when no later page is available.
	NextCursor string `json:"nextCursor"`
}

// DeleteTicketInput selects one ticket for soft deletion.
type DeleteTicketInput struct {
	// TicketID is the positive Zendesk ticket ID.
	TicketID int64 `json:"ticketId"`
}

// DeleteTicketOutput identifies the soft-deleted ticket.
type DeleteTicketOutput struct {
	// TicketID is the deleted ticket's ID.
	TicketID int64 `json:"ticketId"`
}

// RecoverSuspendedTicketInput identifies one suspended ticket to recover.
type RecoverSuspendedTicketInput struct {
	// SuspendedTicketID is the positive ID of the suspended ticket, not its eventual ticket ID.
	SuspendedTicketID int64 `json:"suspendedTicketId"`
}

// RecoverSuspendedTicketOutput contains the recovered ticket.
type RecoverSuspendedTicketOutput struct {
	// Ticket is the ticket Zendesk recovered or a confirmed earlier recovery.
	Ticket Ticket `json:"ticket"`
}

// ListTicketsOperation lists ticket pages.
type ListTicketsOperation struct{ client *Client }

// DeleteTicketOperation soft-deletes one ticket.
type DeleteTicketOperation struct{ client *Client }

// RecoverSuspendedTicketOperation recovers one suspended ticket under a bounded send cap.
type RecoverSuspendedTicketOperation struct{ client *Client }

// ListTickets binds the ticket list Query to this client.
func (client *Client) ListTickets() ListTicketsOperation { return ListTicketsOperation{client} }

// DeleteTicket binds the ticket delete Mutation to this client.
func (client *Client) DeleteTicket() DeleteTicketOperation { return DeleteTicketOperation{client} }

// RecoverSuspendedTicket binds the recovery Mutation to this client.
func (client *Client) RecoverSuspendedTicket() RecoverSuspendedTicketOperation {
	return RecoverSuspendedTicketOperation{client}
}

// Definition returns the generated ticket list Query contract.
func (ListTicketsOperation) Definition() sdkgo.QueryDefinition { return ListTicketsDefinition }

// Definition returns the generated ticket delete Mutation contract.
func (DeleteTicketOperation) Definition() sdkgo.MutationDefinition { return DeleteTicketDefinition }

// Definition returns the generated recovery Mutation contract.
func (RecoverSuspendedTicketOperation) Definition() sdkgo.MutationDefinition {
	return RecoverSuspendedTicketDefinition
}

// IdempotencyKey correlates retries of the same soft delete.
func (DeleteTicketOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteTicketInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates attempts; Zendesk does not deduplicate recoveries with this key.
func (RecoverSuspendedTicketOperation) IdempotencyKey(callID sdkgo.CallID, _ RecoverSuspendedTicketInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one bounded cursor page of tickets.
func (operation ListTicketsOperation) Invoke(call sdkgo.Call, input ListTicketsInput) sdkgo.QueryAttempt[ListTicketsOutput] {
	const operationName = "listTickets"
	if input.PageSize < 0 || input.PageSize > 100 || len(input.Cursor) > maximumCursorLength || strings.ContainsAny(input.Cursor, "\r\n\t") {
		return sdkgo.NewQueryBranch(ListTicketsBranchDefect, ListTicketsOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, operationName, "pageSize must be 1 through 100 or zero, and cursor must be a bounded Zendesk cursor"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationName)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListTicketsBranchDefect, ListTicketsOutput{}, failure, sdkgo.Receipt{})
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 25
	}
	query := url.Values{"page[size]": {strconv.Itoa(pageSize)}}
	if input.Cursor != "" {
		query.Set("page[after]", input.Cursor)
	}
	result := operation.client.exchange(call, credentials, operationName, zendeskRequest{method: http.MethodGet, path: "/tickets", query: query})
	receipt := operation.client.receipt(call, result.response, 0)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRetryable:
		return sdkgo.NewQueryRetry[ListTicketsOutput](result.failure, result.retryAfter)
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(ListTicketsBranchInvalidResponse, ListTicketsOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewQueryBranch(ListTicketsBranchDefect, ListTicketsOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(ListTicketsBranchProviderRejected, ListTicketsOutput{}, &result.failure, receipt)
	}
	output, err := decodeListTicketsPage(result.response.body, operation.client.agentTicketURL, pageSize)
	if err != nil {
		return sdkgo.NewQueryBranch(ListTicketsBranchInvalidResponse, ListTicketsOutput{}, zendeskFailurePointer(sdkgo.FailureProtocol, operationName, "Zendesk returned an invalid ticket page"), receipt)
	}
	return sdkgo.NewQueryBranch(ListTicketsBranchListed, output, nil, receipt)
}

// Invoke soft-deletes one ticket and safely retries an unknown outcome.
func (operation DeleteTicketOperation) Invoke(call sdkgo.Call, input DeleteTicketInput) sdkgo.MutationAttempt[DeleteTicketOutput] {
	const operationName = "deleteTicket"
	if input.TicketID < 1 {
		return sdkgo.NewMutationBranch(DeleteTicketBranchDefect, DeleteTicketOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, operationName, "ticketId must be positive"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationName)
	if failure != nil {
		return sdkgo.NewMutationBranch(DeleteTicketBranchDefect, DeleteTicketOutput{}, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call, credentials, operationName, zendeskRequest{method: http.MethodDelete, path: ticketPath(input.TicketID)})
	receipt := operation.client.receipt(call, result.response, input.TicketID)
	switch result.outcome {
	case exchangeSucceeded:
		if result.response.statusCode != http.StatusNoContent {
			return sdkgo.NewMutationBranch(DeleteTicketBranchProviderRejected, DeleteTicketOutput{}, zendeskFailurePointer(sdkgo.FailureProtocol, operationName, "Zendesk returned an unexpected delete status"), receipt)
		}
		return sdkgo.NewMutationBranch(DeleteTicketBranchDeleted, DeleteTicketOutput{TicketID: input.TicketID}, nil, receipt)
	case exchangeRetryable:
		return sdkgo.NewMutationRetry[DeleteTicketOutput](result.failure, result.retryAfter)
	case exchangeNotFound:
		return sdkgo.NewMutationBranch(DeleteTicketBranchNotFound, DeleteTicketOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewMutationBranch(DeleteTicketBranchDefect, DeleteTicketOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(DeleteTicketBranchProviderRejected, DeleteTicketOutput{}, &result.failure, receipt)
	}
}

// Invoke recovers a suspended ticket and reconciles a known ticket ID before any resend.
func (operation RecoverSuspendedTicketOperation) Invoke(call sdkgo.Call, input RecoverSuspendedTicketInput) sdkgo.MutationAttempt[RecoverSuspendedTicketOutput] {
	const operationName = "recoverSuspendedTicket"
	if input.SuspendedTicketID < 1 {
		return sdkgo.NewMutationBranch(RecoverSuspendedTicketBranchDefect, RecoverSuspendedTicketOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, operationName, "suspendedTicketId must be positive"), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, RecoverSuspendedTicketDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(RecoverSuspendedTicketBranchDefect, RecoverSuspendedTicketOutput{}, zendeskFailurePointer(sdkgo.FailureLocalDefect, operationName, "recovery dispatch fence could not be constructed"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	credentials, failure := operation.client.resolveCredentials(call, operationName)
	if failure != nil {
		if decision.Kind == singledispatch.SendAllowed {
			_ = fence.RecordNotSent()
		}
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(RecoverSuspendedTicketOutput{}, *failure, operation.client.receipt(call, zendeskResponse{}, 0))
		}
		return sdkgo.NewMutationBranch(RecoverSuspendedTicketBranchDefect, RecoverSuspendedTicketOutput{}, failure, sdkgo.Receipt{})
	}
	if decision.Marker.ProviderID != "" && decision.Marker.CallID == call.ID {
		read := operation.client.exchange(call, credentials, operationName, zendeskRequest{method: http.MethodGet, path: "/tickets/" + decision.Marker.ProviderID})
		if read.outcome == exchangeSucceeded {
			decoded, decodeErr := decodeTicketEnvelope(read.response.body, operation.client.agentTicketURL)
			if decodeErr == nil {
				return sdkgo.NewMutationBranch(RecoverSuspendedTicketBranchRecovered, RecoverSuspendedTicketOutput{Ticket: decoded.ticket}, nil, operation.client.receipt(call, read.response, decoded.ticket.ID))
			}
		}
		return sdkgo.NewMutationRetry[RecoverSuspendedTicketOutput](zendeskFailure(sdkgo.FailureAvailability, operationName, "recovered ticket read-back is unavailable"), 0)
	}
	if decision.Kind != singledispatch.SendAllowed && decision.Kind != singledispatch.ReadBackThenResendAllowed {
		return sdkgo.NewMutationUncertain(RecoverSuspendedTicketOutput{}, zendeskFailure(sdkgo.FailureTransport, operationName, "an earlier recovery may have succeeded"), operation.client.receipt(call, zendeskResponse{}, 0))
	}
	var result zendeskExchange
	err = fence.Dispatch(func(ctx context.Context) error {
		result = operation.client.exchangeWithContext(ctx, credentials, operationName, zendeskRequest{method: http.MethodPut, path: "/suspended_tickets/" + strconv.FormatInt(input.SuspendedTicketID, 10) + "/recover"})
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[RecoverSuspendedTicketOutput](zendeskFailure(sdkgo.FailureAvailability, operationName, "recovery dispatch checkpoint is unavailable"), 0)
	}
	receipt := operation.client.receipt(call, result.response, 0)
	if result.response.statusCode == http.StatusTooManyRequests || result.response.statusCode == http.StatusServiceUnavailable {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[RecoverSuspendedTicketOutput](zendeskFailure(sdkgo.FailureAvailability, operationName, "recovery no-send checkpoint is unavailable"), 0)
		}
		return sdkgo.NewMutationRetry[RecoverSuspendedTicketOutput](result.failure, result.retryAfter)
	}
	if result.outcome == exchangeSucceeded && result.response.statusCode == http.StatusOK {
		decoded, decodeErr := decodeTicketEnvelope(result.response.body, operation.client.agentTicketURL)
		if decodeErr == nil {
			// The accepted response remains conclusive if the extra checkpoint is lost.
			_ = fence.RecordProviderID(strconv.FormatInt(decoded.ticket.ID, 10))
			return sdkgo.NewMutationBranch(RecoverSuspendedTicketBranchRecovered, RecoverSuspendedTicketOutput{Ticket: decoded.ticket}, nil, operation.client.receipt(call, result.response, decoded.ticket.ID))
		}
	}
	if decision.MaximumPriorSends == 0 {
		switch result.outcome {
		case exchangeNotFound:
			return sdkgo.NewMutationBranch(RecoverSuspendedTicketBranchNotFound, RecoverSuspendedTicketOutput{}, &result.failure, receipt)
		case exchangeRejected:
			return sdkgo.NewMutationBranch(RecoverSuspendedTicketBranchProviderRejected, RecoverSuspendedTicketOutput{}, &result.failure, receipt)
		case exchangeDefect:
			return sdkgo.NewMutationBranch(RecoverSuspendedTicketBranchDefect, RecoverSuspendedTicketOutput{}, &result.failure, receipt)
		}
	}
	if decision.MaximumPriorSends+1 < call.MaxSends() {
		return sdkgo.NewMutationRetry[RecoverSuspendedTicketOutput](zendeskFailure(sdkgo.FailureTransport, operationName, "recovery outcome is unconfirmed"), result.retryAfter)
	}
	return sdkgo.NewMutationUncertain(RecoverSuspendedTicketOutput{}, zendeskFailure(sdkgo.FailureTransport, operationName, "recovery may have succeeded"), receipt)
}

func decodeListTicketsPage(body []byte, agentTicketURL string, pageSize int) (ListTicketsOutput, error) {
	var envelope struct {
		Tickets []zendeskTicketWire `json:"tickets"`
		Meta    struct {
			HasMore     bool   `json:"has_more"`
			AfterCursor string `json:"after_cursor"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ListTicketsOutput{}, err
	}
	if envelope.Tickets == nil || len(envelope.Tickets) > pageSize {
		return ListTicketsOutput{}, errors.New("ticket page is missing or larger than requested")
	}
	if envelope.Meta.HasMore && envelope.Meta.AfterCursor == "" {
		return ListTicketsOutput{}, errors.New("ticket page has no next cursor")
	}
	tickets := make([]Ticket, 0, len(envelope.Tickets))
	for _, wire := range envelope.Tickets {
		decoded, err := decodeTicketWire(wire, agentTicketURL)
		if err != nil {
			return ListTicketsOutput{}, err
		}
		tickets = append(tickets, decoded.ticket)
	}
	nextCursor := ""
	if envelope.Meta.HasMore {
		nextCursor = envelope.Meta.AfterCursor
	}
	return ListTicketsOutput{Tickets: tickets, NextCursor: nextCursor}, nil
}
