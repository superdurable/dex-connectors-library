// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const updateTicketOperation = "updateTicket"

// UpdateTicketInput is one change to an existing ticket. Zero and empty fields are left
// unchanged; this release cannot clear an agent or group.
type UpdateTicketInput struct {
	// TicketID is the Freshdesk ticket ID.
	TicketID int64 `json:"ticketId"`
	// Status is the new Freshdesk status, including a custom one, or zero to keep it.
	Status TicketStatus `json:"status,omitempty"`
	// Priority is the new Freshdesk priority, or zero to keep it.
	Priority TicketPriority `json:"priority,omitempty"`
	// GroupID assigns a group, or zero to keep the group.
	GroupID int64 `json:"groupId,omitempty"`
	// ResponderID assigns an agent, Freshdesk's responder_id, or zero to keep the agent.
	ResponderID int64 `json:"responderId,omitempty"`
	// AddTags adds tags and keeps the ticket's other tags. A tag already present in any letter case is kept as is.
	AddTags []string `json:"addTags,omitempty"`
	// RemoveTags removes tags in any letter case and keeps the ticket's other tags.
	RemoveTags []string `json:"removeTags,omitempty"`
}

// UpdateTicketOutput is the ticket after the change.
type UpdateTicketOutput struct {
	// Ticket is the ticket Freshdesk returned after the write, or as read when nothing needed writing.
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
	Status      int       `json:"status,omitempty"`
	Priority    int       `json:"priority,omitempty"`
	GroupID     int64     `json:"group_id,omitempty"`
	ResponderID int64     `json:"responder_id,omitempty"`
	Tags        *[]string `json:"tags,omitempty"`
}

// Definition returns the immutable connector operation definition.
func (UpdateTicketOperation) Definition() sdkgo.MutationDefinition { return UpdateTicketDefinition }

// IdempotencyKey uses the stable connector Call ID. Freshdesk documents no idempotency key; the
// write is safe to repeat because it sets absolute values, so the key only correlates the Receipt.
func (UpdateTicketOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTicketInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads the ticket, then sends PUT /api/v2/tickets/{id} with only the fields that differ,
// tags as the complete resulting list. A retried or concurrently dispatched attempt sends the
// same absolute values, or finds them applied and writes nothing.
func (operation UpdateTicketOperation) Invoke(call sdkgo.Call, input UpdateTicketInput) sdkgo.MutationAttempt[UpdateTicketOutput] {
	if err := validateUpdateTicketInput(input); err != nil {
		return sdkgo.NewMutationBranch(UpdateTicketBranchDefect, UpdateTicketOutput{}, freshdeskFailurePointer(sdkgo.FailureValidation, updateTicketOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, updateTicketOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(UpdateTicketBranchDefect, UpdateTicketOutput{}, failure, sdkgo.Receipt{})
	}
	readResult := operation.client.exchange(call, credentials, updateTicketOperation, freshdeskRequest{method: http.MethodGet, path: ticketPath(input.TicketID)})
	receipt := operation.client.receipt(call, readResult.response, input.TicketID)
	if attempt, isTerminal := updateTicketAttemptForExchange(readResult, receipt, UpdateTicketOutput{}); isTerminal {
		return attempt
	}
	current, err := decodeTicketBody(readResult.response.body, input.TicketID)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateTicketBranchInvalidResponse, UpdateTicketOutput{}, freshdeskFailurePointer(sdkgo.FailureProtocol, updateTicketOperation, "Freshdesk returned an invalid ticket: "+err.Error()), receipt)
	}
	currentOutput := UpdateTicketOutput{Ticket: current}
	change, hasEffect := planTicketChange(input, current)
	if !hasEffect {
		return sdkgo.NewMutationBranch(UpdateTicketBranchUpdated, UpdateTicketOutput{Ticket: current, WasAlreadyApplied: true}, nil, receipt)
	}
	writeResult := operation.client.exchange(call, credentials, updateTicketOperation, freshdeskRequest{
		method: http.MethodPut, path: ticketPath(input.TicketID), payload: change,
	})
	receipt = operation.client.receipt(call, writeResult.response, input.TicketID)
	if attempt, isTerminal := updateTicketAttemptForExchange(writeResult, receipt, currentOutput); isTerminal {
		return attempt
	}
	updated, err := decodeTicketBody(writeResult.response.body, input.TicketID)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateTicketBranchInvalidResponse, currentOutput, freshdeskFailurePointer(sdkgo.FailureProtocol, updateTicketOperation, "Freshdesk returned an invalid updated ticket: "+err.Error()), receipt)
	}
	return sdkgo.NewMutationBranch(UpdateTicketBranchUpdated, UpdateTicketOutput{Ticket: updated}, nil, receipt)
}

// updateTicketAttemptForExchange returns the terminal attempt for every outcome except success.
func updateTicketAttemptForExchange(result freshdeskExchange, receipt sdkgo.Receipt, value UpdateTicketOutput) (sdkgo.MutationAttempt[UpdateTicketOutput], bool) {
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
	if input.TicketID < 1 {
		return errors.New("ticketId must be a positive Freshdesk ticket ID")
	}
	if input.Status != 0 {
		if err := validateTicketStatus("status", input.Status); err != nil {
			return err
		}
	}
	if input.Priority != 0 {
		if err := validateTicketPriority("priority", input.Priority); err != nil {
			return err
		}
	}
	if err := errors.Join(validateOptionalID("groupId", input.GroupID), validateOptionalID("responderId", input.ResponderID),
		validateTags("addTags", input.AddTags), validateTags("removeTags", input.RemoveTags)); err != nil {
		return err
	}
	for _, tag := range input.AddTags {
		if containsTagIgnoringCase(input.RemoveTags, tag) {
			return fmt.Errorf("tag %q is in both addTags and removeTags", tag)
		}
	}
	hasRequestedChange := input.Status != 0 || input.Priority != 0 || input.GroupID != 0 || input.ResponderID != 0 ||
		len(input.AddTags) != 0 || len(input.RemoveTags) != 0
	if !hasRequestedChange {
		return errors.New("at least one of status, priority, groupId, responderId, addTags, or removeTags is required")
	}
	return nil
}

// planTicketChange sends only fields that differ from the ticket as read; no difference means already applied.
func planTicketChange(input UpdateTicketInput, current Ticket) (updateTicketRequestWire, bool) {
	change := updateTicketRequestWire{}
	hasEffect := false
	if input.Status != 0 && input.Status != current.Status {
		change.Status, hasEffect = int(input.Status), true
	}
	if input.Priority != 0 && input.Priority != current.Priority {
		change.Priority, hasEffect = int(input.Priority), true
	}
	if input.GroupID != 0 && input.GroupID != current.GroupID {
		change.GroupID, hasEffect = input.GroupID, true
	}
	if input.ResponderID != 0 && input.ResponderID != current.ResponderID {
		change.ResponderID, hasEffect = input.ResponderID, true
	}
	tags := make([]string, 0, len(current.Tags)+len(input.AddTags))
	for _, tag := range current.Tags {
		if !containsTagIgnoringCase(input.RemoveTags, tag) {
			tags = append(tags, tag)
		}
	}
	for _, tag := range input.AddTags {
		if !containsTagIgnoringCase(tags, tag) {
			tags = append(tags, tag)
		}
	}
	if !slices.Equal(tags, current.Tags) {
		change.Tags, hasEffect = &tags, true
	}
	return change, hasEffect
}

func containsTagIgnoringCase(tags []string, tag string) bool {
	return slices.ContainsFunc(tags, func(candidate string) bool { return strings.EqualFold(candidate, tag) })
}
