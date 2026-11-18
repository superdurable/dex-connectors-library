// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias

import (
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const updateTicketOperation = "updateTicket"

// UpdateTicketInput is one change to an existing ticket. Empty and zero fields are left
// unchanged; this release cannot unassign a user or team.
type UpdateTicketInput struct {
	// TicketID is the Gorgias ticket ID.
	TicketID int64 `json:"ticketId"`
	// Status is open or closed, or empty to keep it.
	Status TicketStatus `json:"status,omitempty"`
	// Priority is the new Gorgias priority, or empty to keep it.
	Priority TicketPriority `json:"priority,omitempty"`
	// AssigneeUserID assigns a Gorgias user, or zero to keep the assignee.
	AssigneeUserID int64 `json:"assigneeUserId,omitempty"`
	// AssigneeTeamID assigns a Gorgias team, or zero to keep the team.
	AssigneeTeamID int64 `json:"assigneeTeamId,omitempty"`
	// AddTags adds these tag names and keeps the ticket's other tags. Names compare case sensitively.
	AddTags []string `json:"addTags,omitempty"`
	// RemoveTags removes these tag names and keeps the ticket's other tags.
	RemoveTags []string `json:"removeTags,omitempty"`
}

// UpdateTicketOutput is the ticket after the change.
type UpdateTicketOutput struct {
	// Ticket is the ticket as Gorgias returned it after the last write, or as read when nothing needed writing.
	// On a failure branch that follows an applied tag change, it is the ticket as read again after the
	// failure, so it shows that change; when that read fails, it is the ticket as first read.
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
	Status       string         `json:"status,omitempty"`
	Priority     string         `json:"priority,omitempty"`
	AssigneeUser *gorgiasIDWire `json:"assignee_user,omitempty"`
	AssigneeTeam *gorgiasIDWire `json:"assignee_team,omitempty"`
}

type ticketTagNamesWire struct {
	Names []string `json:"names"`
}

// ticketChange is the part of an UpdateTicketInput that differs from the ticket as read.
type ticketChange struct {
	fields     updateTicketRequestWire
	hasFields  bool
	addTags    []string
	removeTags []string
}

// Definition returns the immutable connector operation definition.
func (UpdateTicketOperation) Definition() sdkgo.MutationDefinition { return UpdateTicketDefinition }

// IdempotencyKey uses the stable connector Call ID. Gorgias documents no idempotency key; the
// writes are safe to repeat because they set absolute values and tag sets, so the key only
// correlates the Receipt.
func (UpdateTicketOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTicketInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads the ticket, adds and removes only the tags that differ with POST and DELETE
// /api/tickets/{id}/tags, then sends PUT /api/tickets/{id} with only the fields that differ.
// A retried or concurrently dispatched attempt sends the same values, or finds them applied. A
// tag change stays applied when a later write fails conclusively, so that failure branch carries
// the ticket as read again.
func (operation UpdateTicketOperation) Invoke(call sdkgo.Call, input UpdateTicketInput) sdkgo.MutationAttempt[UpdateTicketOutput] {
	if err := validateUpdateTicketInput(input); err != nil {
		return sdkgo.NewMutationBranch(UpdateTicketBranchDefect, UpdateTicketOutput{}, gorgiasFailurePointer(sdkgo.FailureValidation, updateTicketOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure, isTerminal := operation.client.resolveWriteCredentials(call, updateTicketOperation)
	if failure != nil && !isTerminal {
		return sdkgo.NewMutationRetry[UpdateTicketOutput](*failure, 0)
	}
	if failure != nil {
		return sdkgo.NewMutationBranch(UpdateTicketBranchDefect, UpdateTicketOutput{}, failure, sdkgo.Receipt{})
	}
	current, attempt, isTerminal := operation.readTicket(call, credentials, input.TicketID, UpdateTicketOutput{}, false)
	if isTerminal {
		return attempt
	}
	change := planTicketChange(input, current)
	if !change.hasFields && len(change.addTags) == 0 && len(change.removeTags) == 0 {
		return sdkgo.NewMutationBranch(UpdateTicketBranchUpdated, UpdateTicketOutput{Ticket: current, WasAlreadyApplied: true}, nil,
			operation.client.receipt(call, gorgiasResponse{}, current.ID))
	}
	currentOutput := UpdateTicketOutput{Ticket: current}
	writes := []gorgiasRequest{}
	if len(change.addTags) != 0 {
		writes = append(writes, gorgiasRequest{method: http.MethodPost, path: ticketPath(input.TicketID) + "/tags", payload: ticketTagNamesWire{Names: change.addTags}})
	}
	if len(change.removeTags) != 0 {
		writes = append(writes, gorgiasRequest{method: http.MethodDelete, path: ticketPath(input.TicketID) + "/tags", payload: ticketTagNamesWire{Names: change.removeTags}})
	}
	if change.hasFields {
		writes = append(writes, gorgiasRequest{method: http.MethodPut, path: ticketPath(input.TicketID), payload: change.fields})
	}
	hasAppliedWrite := false
	for _, write := range writes {
		result := operation.client.exchange(call, credentials, updateTicketOperation, write)
		receipt := operation.client.receipt(call, result.response, input.TicketID)
		if hasAppliedWrite && isConclusiveWriteFailure(result) {
			currentOutput = operation.rereadAfterPartialChange(call, credentials, input.TicketID, currentOutput)
		}
		if attempt, isTerminal := updateTicketAttemptForExchange(result, receipt, currentOutput, true); isTerminal {
			return attempt
		}
		hasAppliedWrite = true
		if write.method == http.MethodPut {
			return operation.decodeUpdatedTicket(call, result, input.TicketID)
		}
	}
	updated, attempt, isTerminal := operation.readTicket(call, credentials, input.TicketID, currentOutput, true)
	if isTerminal {
		return attempt
	}
	return sdkgo.NewMutationBranch(UpdateTicketBranchUpdated, UpdateTicketOutput{Ticket: updated}, nil, operation.client.receipt(call, gorgiasResponse{}, updated.ID))
}

// readTicket reads GET /api/tickets/{id}, returning a terminal attempt for every outcome except a valid ticket.
// After a write, an unknown read outcome retries the absolute change; invalidResponse is only for the first read.
func (operation UpdateTicketOperation) readTicket(call sdkgo.Call, credentials Credentials, ticketID int64, currentOutput UpdateTicketOutput, isAfterWrite bool) (Ticket, sdkgo.MutationAttempt[UpdateTicketOutput], bool) {
	result := operation.client.exchange(call, credentials, updateTicketOperation, gorgiasRequest{method: http.MethodGet, path: ticketPath(ticketID)})
	receipt := operation.client.receipt(call, result.response, ticketID)
	if attempt, isTerminal := updateTicketAttemptForExchange(result, receipt, currentOutput, isAfterWrite); isTerminal {
		return Ticket{}, attempt, true
	}
	ticket, _, err := decodeTicketBody(result.response.body, ticketID)
	switch {
	case err != nil && isAfterWrite:
		return Ticket{}, sdkgo.NewMutationRetry[UpdateTicketOutput](gorgiasFailure(sdkgo.FailureProtocol, updateTicketOperation,
			"Gorgias returned an invalid ticket after the change: "+err.Error()), 0), true
	case err != nil:
		return Ticket{}, sdkgo.NewMutationBranch(UpdateTicketBranchInvalidResponse, currentOutput,
			gorgiasFailurePointer(sdkgo.FailureProtocol, updateTicketOperation, "Gorgias returned an invalid ticket: "+err.Error()), receipt), true
	}
	return ticket, sdkgo.MutationAttempt[UpdateTicketOutput]{}, false
}

// rereadAfterPartialChange shows a failure branch the tags already written; an unreadable ticket keeps currentOutput.
func (operation UpdateTicketOperation) rereadAfterPartialChange(call sdkgo.Call, credentials Credentials, ticketID int64, currentOutput UpdateTicketOutput) UpdateTicketOutput {
	ticket, _, isTerminal := operation.readTicket(call, credentials, ticketID, currentOutput, true)
	if isTerminal {
		return currentOutput
	}
	return UpdateTicketOutput{Ticket: ticket}
}

func (operation UpdateTicketOperation) decodeUpdatedTicket(call sdkgo.Call, result gorgiasExchange, ticketID int64) sdkgo.MutationAttempt[UpdateTicketOutput] {
	receipt := operation.client.receipt(call, result.response, ticketID)
	updated, _, err := decodeTicketBody(result.response.body, ticketID)
	if err != nil {
		// The PUT sets final values, so an unusable answer repeats it.
		return sdkgo.NewMutationRetry[UpdateTicketOutput](gorgiasFailure(sdkgo.FailureProtocol, updateTicketOperation,
			"Gorgias accepted the change but returned an invalid updated ticket: "+err.Error()), 0)
	}
	return sdkgo.NewMutationBranch(UpdateTicketBranchUpdated, UpdateTicketOutput{Ticket: updated}, nil, receipt)
}

// updateTicketAttemptForExchange returns the terminal attempt for every outcome except success. After a
// write may have been sent, only a definite refusal selects a branch; any other failure retries the change.
func updateTicketAttemptForExchange(result gorgiasExchange, receipt sdkgo.Receipt, currentOutput UpdateTicketOutput, isAfterWrite bool) (sdkgo.MutationAttempt[UpdateTicketOutput], bool) {
	if isAfterWrite && result.outcome != exchangeSucceeded && !isConclusiveWriteFailure(result) {
		return sdkgo.NewMutationRetry[UpdateTicketOutput](result.failure, result.retryAfter), true
	}
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[UpdateTicketOutput]{}, false
	case exchangeRateLimited, exchangeNotSent, exchangeUnavailable:
		return sdkgo.NewMutationRetry[UpdateTicketOutput](result.failure, result.retryAfter), true
	case exchangeNotFound:
		return sdkgo.NewMutationBranch(UpdateTicketBranchNotFound, currentOutput, &result.failure, receipt), true
	case exchangeInvalid:
		return sdkgo.NewMutationBranch(UpdateTicketBranchInvalidResponse, currentOutput, &result.failure, receipt), true
	case exchangeDefect:
		return sdkgo.NewMutationBranch(UpdateTicketBranchDefect, currentOutput, &result.failure, receipt), true
	default:
		return sdkgo.NewMutationBranch(UpdateTicketBranchProviderRejected, currentOutput, &result.failure, receipt), true
	}
}

// isConclusiveWriteFailure reports a definite refusal or a request that was never built.
func isConclusiveWriteFailure(result gorgiasExchange) bool {
	switch result.outcome {
	case exchangeDefect:
		return true
	case exchangeNotFound, exchangeRejected:
		return isDefiniteRefusal(result)
	default:
		return false
	}
}

func validateUpdateTicketInput(input UpdateTicketInput) error {
	if input.TicketID < 1 {
		return errors.New("ticketId must be a positive Gorgias ticket ID")
	}
	if input.Status != "" {
		if err := validateTicketStatus("status", input.Status); err != nil {
			return err
		}
	}
	if input.Priority != "" {
		if err := validateTicketPriority("priority", input.Priority); err != nil {
			return err
		}
	}
	if err := errors.Join(validateOptionalID("assigneeUserId", input.AssigneeUserID), validateOptionalID("assigneeTeamId", input.AssigneeTeamID),
		validateTags("addTags", input.AddTags), validateTags("removeTags", input.RemoveTags)); err != nil {
		return err
	}
	for _, tag := range input.AddTags {
		if slices.Contains(input.RemoveTags, tag) {
			return fmt.Errorf("tag %q is in both addTags and removeTags", tag)
		}
	}
	hasRequestedChange := input.Status != "" || input.Priority != "" || input.AssigneeUserID != 0 || input.AssigneeTeamID != 0 ||
		len(input.AddTags) != 0 || len(input.RemoveTags) != 0
	if !hasRequestedChange {
		return errors.New("at least one of status, priority, assigneeUserId, assigneeTeamId, addTags, or removeTags is required")
	}
	return nil
}

// planTicketChange keeps only fields and tags that differ from the ticket as read.
func planTicketChange(input UpdateTicketInput, current Ticket) ticketChange {
	change := ticketChange{}
	if input.Status != "" && input.Status != current.Status {
		change.fields.Status, change.hasFields = string(input.Status), true
	}
	if input.Priority != "" && input.Priority != current.Priority {
		change.fields.Priority, change.hasFields = string(input.Priority), true
	}
	if input.AssigneeUserID != 0 && input.AssigneeUserID != current.AssigneeUserID {
		change.fields.AssigneeUser, change.hasFields = &gorgiasIDWire{ID: input.AssigneeUserID}, true
	}
	if input.AssigneeTeamID != 0 && input.AssigneeTeamID != current.AssigneeTeamID {
		change.fields.AssigneeTeam, change.hasFields = &gorgiasIDWire{ID: input.AssigneeTeamID}, true
	}
	for _, tag := range input.AddTags {
		if !slices.Contains(current.Tags, tag) {
			change.addTags = append(change.addTags, tag)
		}
	}
	for _, tag := range input.RemoveTags {
		if slices.Contains(current.Tags, tag) {
			change.removeTags = append(change.removeTags, tag)
		}
	}
	return change
}
