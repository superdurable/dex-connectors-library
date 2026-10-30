// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk

import (
	"errors"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const updateTicketOperation = "updateTicket"

// UpdateTicketInput is one change to an existing ticket. Empty fields are left unchanged; this
// release cannot clear an assignee.
type UpdateTicketInput struct {
	// TicketID is the Zoho Desk ticket ID.
	TicketID string `json:"ticketId"`
	// Status is the new Zoho Desk status name, including a custom one, or empty to keep it.
	Status TicketStatus `json:"status,omitempty"`
	// Priority is the new Zoho Desk priority name, or empty to keep it.
	Priority TicketPriority `json:"priority,omitempty"`
	// AssigneeID assigns an agent of the ticket's department, or empty to keep the assignee.
	AssigneeID string `json:"assigneeId,omitempty"`
	// DepartmentID moves the ticket to another department, or empty to keep it. Zoho Desk applies
	// the new department's rules, so set AssigneeID too when the agent must stay assigned.
	DepartmentID string `json:"departmentId,omitempty"`
}

// UpdateTicketOutput is the ticket after the change.
type UpdateTicketOutput struct {
	// Ticket is the ticket Zoho Desk returned after the write, or as read when nothing needed writing.
	Ticket Ticket `json:"ticket"`
	// WasAlreadyApplied reports that the ticket already held every requested value, for example
	// because an earlier attempt of this Step wrote them, so this attempt wrote nothing.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
}

// UpdateTicketOperation is the updateTicket Mutation.
type UpdateTicketOperation struct {
	client *Client
}

type updateTicketRequestWire struct {
	Status       string `json:"status,omitempty"`
	Priority     string `json:"priority,omitempty"`
	AssigneeID   string `json:"assigneeId,omitempty"`
	DepartmentID string `json:"departmentId,omitempty"`
}

// Definition returns the immutable connector operation definition.
func (UpdateTicketOperation) Definition() sdkgo.MutationDefinition { return UpdateTicketDefinition }

// IdempotencyKey uses the stable connector Call ID. Zoho Desk documents no idempotency key; the
// write is safe to repeat because it sets absolute values, so the key only correlates the Receipt.
func (UpdateTicketOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTicketInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads GET /api/v1/tickets/{id}, then sends PATCH /api/v1/tickets/{id} with only the fields
// that differ. A retried or concurrently dispatched attempt sends the same absolute values, or
// finds them applied and writes nothing.
func (operation UpdateTicketOperation) Invoke(call sdkgo.Call, input UpdateTicketInput) sdkgo.MutationAttempt[UpdateTicketOutput] {
	if err := validateUpdateTicketInput(input); err != nil {
		return sdkgo.NewMutationBranch(UpdateTicketBranchDefect, UpdateTicketOutput{}, deskFailurePointer(sdkgo.FailureValidation, updateTicketOperation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, failure := operation.client.startSession(call, updateTicketOperation)
	if failure != nil {
		return updateTicketAttemptForSession(failure)
	}
	defer cancel()
	readResult := operation.client.exchange(session, updateTicketOperation, deskRequest{method: http.MethodGet, path: ticketPath(input.TicketID)})
	receipt := operation.client.receipt(call, readResult.response, input.TicketID)
	if attempt, isTerminal := updateTicketAttemptForExchange(readResult, receipt, UpdateTicketOutput{}); isTerminal {
		return attempt
	}
	current, _, err := decodeTicketBody(readResult.response.body, input.TicketID)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateTicketBranchInvalidResponse, UpdateTicketOutput{}, deskFailurePointer(sdkgo.FailureProtocol, updateTicketOperation, "Zoho Desk returned an invalid ticket: "+err.Error()), receipt)
	}
	currentOutput := UpdateTicketOutput{Ticket: current}
	change, hasEffect := planTicketChange(input, current)
	if !hasEffect {
		return sdkgo.NewMutationBranch(UpdateTicketBranchUpdated, UpdateTicketOutput{Ticket: current, WasAlreadyApplied: true}, nil, receipt)
	}
	writeResult := operation.client.exchange(session, updateTicketOperation, deskRequest{
		method: http.MethodPatch, path: ticketPath(input.TicketID), payload: change,
	})
	receipt = operation.client.receipt(call, writeResult.response, input.TicketID)
	if attempt, isTerminal := updateTicketAttemptForExchange(writeResult, receipt, currentOutput); isTerminal {
		return attempt
	}
	updated, _, err := decodeTicketBody(writeResult.response.body, input.TicketID)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateTicketBranchInvalidResponse, currentOutput, deskFailurePointer(sdkgo.FailureProtocol, updateTicketOperation, "Zoho Desk returned an invalid updated ticket: "+err.Error()), receipt)
	}
	return sdkgo.NewMutationBranch(UpdateTicketBranchUpdated, UpdateTicketOutput{Ticket: updated}, nil, receipt)
}

func updateTicketAttemptForSession(failure *sessionFailure) sdkgo.MutationAttempt[UpdateTicketOutput] {
	switch failure.route {
	case sessionRetry:
		return sdkgo.NewMutationRetry[UpdateTicketOutput](failure.failure, 0)
	case sessionRejected:
		return sdkgo.NewMutationBranch(UpdateTicketBranchProviderRejected, UpdateTicketOutput{}, &failure.failure, sdkgo.Receipt{})
	default:
		return sdkgo.NewMutationBranch(UpdateTicketBranchDefect, UpdateTicketOutput{}, &failure.failure, sdkgo.Receipt{})
	}
}

// updateTicketAttemptForExchange returns the terminal attempt for every outcome except success.
func updateTicketAttemptForExchange(result deskExchange, receipt sdkgo.Receipt, value UpdateTicketOutput) (sdkgo.MutationAttempt[UpdateTicketOutput], bool) {
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[UpdateTicketOutput]{}, false
	case exchangeRateLimited, exchangeNotSent, exchangeUnavailable:
		return sdkgo.NewMutationRetry[UpdateTicketOutput](result.failure, result.retryAfter), true
	case exchangeNotFound:
		return sdkgo.NewMutationBranch(UpdateTicketBranchNotFound, value, &result.failure, receipt), true
	case exchangeInvalid:
		return sdkgo.NewMutationBranch(UpdateTicketBranchInvalidResponse, value, &result.failure, receipt), true
	case exchangeDefect:
		return sdkgo.NewMutationBranch(UpdateTicketBranchDefect, value, &result.failure, receipt), true
	default:
		return sdkgo.NewMutationBranch(UpdateTicketBranchProviderRejected, value, &result.failure, receipt), true
	}
}

func validateUpdateTicketInput(input UpdateTicketInput) error {
	if err := validateZohoID("ticketId", input.TicketID); err != nil {
		return err
	}
	if input.Status != "" {
		if err := validatePicklistValue("status", string(input.Status)); err != nil {
			return err
		}
	}
	if input.Priority != "" {
		if err := validatePicklistValue("priority", string(input.Priority)); err != nil {
			return err
		}
	}
	if err := errors.Join(validateOptionalZohoID("assigneeId", input.AssigneeID), validateOptionalZohoID("departmentId", input.DepartmentID)); err != nil {
		return err
	}
	if input == (UpdateTicketInput{TicketID: input.TicketID}) {
		return errors.New("at least one of status, priority, assigneeId, or departmentId is required")
	}
	return nil
}

// planTicketChange sends only fields that differ from the ticket as read; names compare without
// regard to letter case, as Zoho Desk's picklists do. No difference means already applied.
func planTicketChange(input UpdateTicketInput, current Ticket) (updateTicketRequestWire, bool) {
	change := updateTicketRequestWire{}
	hasEffect := false
	if input.Status != "" && !strings.EqualFold(string(input.Status), string(current.Status)) {
		change.Status, hasEffect = string(input.Status), true
	}
	if input.Priority != "" && !strings.EqualFold(string(input.Priority), string(current.Priority)) {
		change.Priority, hasEffect = string(input.Priority), true
	}
	if input.DepartmentID != "" && input.DepartmentID != current.DepartmentID {
		change.DepartmentID, hasEffect = input.DepartmentID, true
	}
	if input.AssigneeID != "" && (input.AssigneeID != current.AssigneeID || change.DepartmentID != "") {
		change.AssigneeID, hasEffect = input.AssigneeID, true
	}
	return change, hasEffect
}
