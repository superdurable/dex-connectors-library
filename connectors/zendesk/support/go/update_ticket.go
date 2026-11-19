// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const updateTicketOperation = "updateTicket"

// UpdateTicketInput is one change to an existing ticket. Empty and zero fields
// are left unchanged; this release cannot clear an assignee or group.
type UpdateTicketInput struct {
	// TicketID is the Zendesk ticket ID.
	TicketID int64 `json:"ticketId"`
	// Status is the new Zendesk status, or empty to keep it.
	Status TicketStatus `json:"status,omitempty"`
	// Priority is the new Zendesk priority, or empty to keep it.
	Priority TicketPriority `json:"priority,omitempty"`
	// AssigneeID assigns an agent, or zero to keep the assignee.
	AssigneeID int64 `json:"assigneeId,omitempty"`
	// GroupID assigns a group, or zero to keep the group.
	GroupID int64 `json:"groupId,omitempty"`
	// AddTags adds lowercase tags and keeps the ticket's other tags.
	AddTags []string `json:"addTags,omitempty"`
	// RemoveTags removes lowercase tags and keeps the ticket's other tags.
	RemoveTags []string `json:"removeTags,omitempty"`
	// Comment adds one public reply or internal note, or nil for none.
	Comment *TicketCommentInput `json:"comment,omitempty"`
	// ExpectedUpdatedAt is the ticket's UpdatedAt as the application last read it, an RFC 3339
	// instant such as 2026-01-28T08:45:00Z. A later change selects conflict; blank writes regardless.
	ExpectedUpdatedAt string `json:"expectedUpdatedAt,omitempty"`
}

// UpdateTicketOutput is the ticket after the change.
type UpdateTicketOutput struct {
	// Ticket is the ticket Zendesk returned after the change, or as read when nothing needed writing.
	Ticket Ticket `json:"ticket"`
	// WasAlreadyApplied reports that an earlier attempt of this Step already wrote the change, or
	// the ticket already held every requested value, so this attempt wrote nothing.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
}

// UpdateTicketOperation is the updateTicket Mutation.
type UpdateTicketOperation struct {
	client *Client
}

type updateTicketRequestWire struct {
	Ticket updateTicketWire `json:"ticket"`
}

type updateTicketWire struct {
	Status       string             `json:"status,omitempty"`
	Priority     string             `json:"priority,omitempty"`
	AssigneeID   int64              `json:"assignee_id,omitempty"`
	GroupID      int64              `json:"group_id,omitempty"`
	Tags         *[]string          `json:"tags,omitempty"`
	Comment      *ticketCommentWire `json:"comment,omitempty"`
	Metadata     map[string]string  `json:"metadata"`
	SafeUpdate   bool               `json:"safe_update"`
	UpdatedStamp string             `json:"updated_stamp"`
}

// Definition returns the immutable connector operation definition.
func (UpdateTicketOperation) Definition() sdkgo.MutationDefinition { return UpdateTicketDefinition }

// IdempotencyKey uses the stable connector Call ID. The key is stored as ticket audit
// metadata, which is how a retried attempt recognizes its own earlier change.
func (UpdateTicketOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTicketInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke fences the write and, on every attempt, reads the ticket and looks for an audit carrying
// this call's key when a comment or expectedUpdatedAt makes a repeat observable. It writes only
// fields that differ, as a Zendesk safe update against the updated_at it read. An unknown write
// outcome, including a 409, returns Retry, also on the last allowed send, so the next attempt reads
// back first: below the cap it resends an unconfirmed write, and at the cap it selects uncertain. A 2xx
// is accepted and never resent: an unusable 2xx saves the ticket ID and reads the change back, and
// every later attempt only reads back, selecting updated when it confirms the change and uncertain
// when it cannot.
func (operation UpdateTicketOperation) Invoke(call sdkgo.Call, input UpdateTicketInput) sdkgo.MutationAttempt[UpdateTicketOutput] {
	if err := validateUpdateTicketInput(input); err != nil {
		return sdkgo.NewMutationBranch(UpdateTicketBranchDefect, UpdateTicketOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, updateTicketOperation, err.Error()), sdkgo.Receipt{})
	}
	if call.IdempotencyKey == "" {
		return sdkgo.NewMutationBranch(UpdateTicketBranchDefect, UpdateTicketOutput{}, zendeskFailurePointer(sdkgo.FailureLocalDefect, updateTicketOperation, "connector call has no idempotency key"), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, UpdateTicketDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateTicketBranchDefect, UpdateTicketOutput{}, zendeskFailurePointer(sdkgo.FailureLocalDefect, updateTicketOperation, "single-dispatch fence could not start"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		// FailClosed sends nothing, so a later attempt decides again from a readable checkpoint.
		return sdkgo.NewMutationRetry[UpdateTicketOutput](zendeskFailure(sdkgo.FailureAvailability, updateTicketOperation,
			"the single-dispatch checkpoint could not be read; nothing was sent in this attempt"), 0)
	}
	permit := newTicketWritePermit(fence, decision)
	credentials, failure, isTerminal := operation.client.resolveWriteCredentials(call, updateTicketOperation)
	if failure != nil {
		return permit.credentialFailureAttempt(*failure, isTerminal)
	}
	if permit.hasAcceptedWrite {
		// Zendesk accepted an earlier attempt's PUT, so this attempt only reads it back.
		return acceptedTicketWrite{operation: operation, call: call, credentials: credentials, input: input,
			canRetryTransientRead: true, isFromEarlierAttempt: true}.readBack()
	}
	readResult := operation.client.exchange(call, credentials, updateTicketOperation, zendeskRequest{method: http.MethodGet, path: ticketPath(input.TicketID)})
	receipt := operation.client.receipt(call, readResult.response, input.TicketID)
	if attempt, isTerminal := permit.readAttempt(readResult, receipt, UpdateTicketOutput{}); isTerminal {
		return attempt
	}
	current, err := decodeTicketEnvelope(readResult.response.body, operation.client.agentTicketURL)
	if err == nil && current.ticket.ID != input.TicketID {
		err = errors.New("ticket response is for another ticket")
	}
	if err != nil {
		invalid := zendeskFailure(sdkgo.FailureProtocol, updateTicketOperation, "Zendesk returned an invalid ticket: "+err.Error())
		if !permit.isFirstSend {
			return permit.unconfirmedRead(UpdateTicketOutput{}, invalid, receipt)
		}
		return sdkgo.NewMutationBranch(UpdateTicketBranchInvalidResponse, UpdateTicketOutput{}, &invalid, receipt)
	}
	currentOutput := UpdateTicketOutput{Ticket: current.ticket}
	if input.Comment != nil || input.ExpectedUpdatedAt != "" {
		auditResult := operation.client.exchange(call, credentials, updateTicketOperation, zendeskRequest{
			method: http.MethodGet, path: ticketPath(input.TicketID) + "/audits", query: url.Values{"sort_order": {"desc"}},
		})
		receipt = operation.client.receipt(call, auditResult.response, input.TicketID)
		if attempt, isTerminal := permit.readAttempt(auditResult, receipt, currentOutput); isTerminal {
			return attempt
		}
		hasEarlierChange, err := hasAuditWithIdempotencyKey(auditResult.response.body, call.IdempotencyKey)
		if err != nil {
			invalid := zendeskFailure(sdkgo.FailureProtocol, updateTicketOperation, "Zendesk returned an invalid audit page: "+err.Error())
			if !permit.isFirstSend {
				return permit.unconfirmedRead(currentOutput, invalid, receipt)
			}
			return sdkgo.NewMutationBranch(UpdateTicketBranchInvalidResponse, currentOutput, &invalid, receipt)
		}
		if hasEarlierChange {
			return sdkgo.NewMutationBranch(UpdateTicketBranchUpdated, UpdateTicketOutput{Ticket: current.ticket, WasAlreadyApplied: true}, nil, receipt)
		}
	}
	if input.ExpectedUpdatedAt != "" {
		// validateUpdateTicketInput already rejected an unparsable value.
		expectedUpdatedAt, _ := time.Parse(time.RFC3339, input.ExpectedUpdatedAt)
		if !expectedUpdatedAt.Equal(current.ticket.UpdatedAt) {
			conflict := zendeskFailure(sdkgo.FailureConflict, updateTicketOperation,
				"ticket was updated at "+current.ticket.UpdatedAt.Format(time.RFC3339)+", after expectedUpdatedAt")
			if !permit.isFirstSend {
				// An earlier send of this Step may have moved updated_at without an audit on the latest page.
				return permit.unconfirmedRead(currentOutput, conflict, receipt)
			}
			return sdkgo.NewMutationBranch(UpdateTicketBranchConflict, currentOutput, &conflict, receipt)
		}
	}
	change, hasEffect := planTicketChange(input, current, call.IdempotencyKey)
	if !hasEffect {
		return sdkgo.NewMutationBranch(UpdateTicketBranchUpdated, UpdateTicketOutput{Ticket: current.ticket, WasAlreadyApplied: true}, nil, receipt)
	}
	if permit.isAtSendCap {
		return sdkgo.NewMutationUncertain(currentOutput, zendeskFailure(sdkgo.FailureTransport, updateTicketOperation,
			"earlier attempts may have written the change and the send cap is reached, and the latest audit page shows no change by this Step"), receipt)
	}
	write := &fencedTicketWrite{client: operation.client, call: call, credentials: credentials,
		request: zendeskRequest{method: http.MethodPut, path: ticketPath(input.TicketID), payload: updateTicketRequestWire{Ticket: change}}}
	if err := fence.Dispatch(write.dispatch); err != nil {
		// Dispatch refused before sending, so nothing reached Zendesk in this attempt.
		return sdkgo.NewMutationRetry[UpdateTicketOutput](zendeskFailure(sdkgo.FailureAvailability, updateTicketOperation, "Zendesk write was not sent before the attempt deadline or checkpoint"), 0)
	}
	writeResult := write.result
	receipt = operation.client.receipt(call, writeResult.response, input.TicketID)
	if !isSuccessStatus(writeResult.response.statusCode) {
		return permit.writeAttempt(writeResult, receipt, currentOutput)
	}
	if writeResult.outcome == exchangeSucceeded {
		updated, err := decodeTicketEnvelope(writeResult.response.body, operation.client.agentTicketURL)
		if err == nil && updated.ticket.ID == input.TicketID {
			return sdkgo.NewMutationBranch(UpdateTicketBranchUpdated, UpdateTicketOutput{Ticket: updated.ticket}, nil, receipt)
		}
	}
	// Zendesk accepted the PUT, so it is never resent; the saved ticket ID makes later attempts only read back.
	isTicketIDRecorded := fence.RecordProviderID(strconv.FormatInt(input.TicketID, 10)) == nil
	return acceptedTicketWrite{operation: operation, call: call, credentials: credentials, input: input,
		canRetryTransientRead: isTicketIDRecorded}.readBack()
}

// acceptedTicketWrite confirms a PUT that Zendesk answered with 2xx whose answer was unusable, or that an
// earlier attempt saved; it never sends.
type acceptedTicketWrite struct {
	operation   UpdateTicketOperation
	call        sdkgo.Call
	credentials Credentials
	input       UpdateTicketInput
	// canRetryTransientRead means the ticket ID is checkpointed, so a Retry can only read back again.
	canRetryTransientRead bool
	// isFromEarlierAttempt reports a confirmed change as WasAlreadyApplied.
	isFromEarlierAttempt bool
}

// readBack reads the ticket and its latest audits: this Step's audit, or a ticket without a pending field
// change when no comment was requested, confirms the write; anything else selects uncertain.
func (write acceptedTicketWrite) readBack() sdkgo.MutationAttempt[UpdateTicketOutput] {
	client := write.operation.client
	readResult := client.exchange(write.call, write.credentials, updateTicketOperation, zendeskRequest{method: http.MethodGet, path: ticketPath(write.input.TicketID)})
	receipt := client.receipt(write.call, readResult.response, write.input.TicketID)
	if attempt, isTerminal := write.readAttempt(readResult, receipt, UpdateTicketOutput{}); isTerminal {
		return attempt
	}
	current, err := decodeTicketEnvelope(readResult.response.body, client.agentTicketURL)
	if err == nil && current.ticket.ID != write.input.TicketID {
		err = errors.New("ticket response is for another ticket")
	}
	if err != nil {
		return write.unconfirmed(UpdateTicketOutput{}, sdkgo.FailureProtocol, "Zendesk returned an invalid ticket: "+err.Error(), receipt)
	}
	currentOutput := UpdateTicketOutput{Ticket: current.ticket}
	if write.input.Comment != nil || write.input.ExpectedUpdatedAt != "" {
		auditResult := client.exchange(write.call, write.credentials, updateTicketOperation, zendeskRequest{
			method: http.MethodGet, path: ticketPath(write.input.TicketID) + "/audits", query: url.Values{"sort_order": {"desc"}},
		})
		receipt = client.receipt(write.call, auditResult.response, write.input.TicketID)
		if attempt, isTerminal := write.readAttempt(auditResult, receipt, currentOutput); isTerminal {
			return attempt
		}
		hasChange, err := hasAuditWithIdempotencyKey(auditResult.response.body, write.call.IdempotencyKey)
		if err != nil {
			return write.unconfirmed(currentOutput, sdkgo.FailureProtocol, "Zendesk returned an invalid audit page: "+err.Error(), receipt)
		}
		if hasChange {
			return write.confirmed(current.ticket, receipt)
		}
	}
	if write.input.Comment == nil {
		if _, hasEffect := planTicketChange(write.input, current, write.call.IdempotencyKey); !hasEffect {
			return write.confirmed(current.ticket, receipt)
		}
	}
	return write.unconfirmed(currentOutput, sdkgo.FailureTransport, "the ticket and its latest audit page show no change by this Step", receipt)
}

// readAttempt retries a transient read only when that cannot resend; every other failed read selects uncertain.
func (write acceptedTicketWrite) readAttempt(result zendeskExchange, receipt sdkgo.Receipt, value UpdateTicketOutput) (sdkgo.MutationAttempt[UpdateTicketOutput], bool) {
	switch {
	case result.outcome == exchangeSucceeded:
		return sdkgo.MutationAttempt[UpdateTicketOutput]{}, false
	case (result.outcome == exchangeRetryable || result.outcome == exchangeNotSent) && write.canRetryTransientRead:
		return sdkgo.NewMutationRetry[UpdateTicketOutput](result.failure, result.retryAfter), true
	}
	return write.unconfirmed(value, result.failure.Kind, result.failure.Message, receipt), true
}

func (write acceptedTicketWrite) confirmed(ticket Ticket, receipt sdkgo.Receipt) sdkgo.MutationAttempt[UpdateTicketOutput] {
	return sdkgo.NewMutationBranch(UpdateTicketBranchUpdated, UpdateTicketOutput{Ticket: ticket, WasAlreadyApplied: write.isFromEarlierAttempt}, nil, receipt)
}

func (write acceptedTicketWrite) unconfirmed(value UpdateTicketOutput, kind sdkgo.FailureKind, reason string, receipt sdkgo.Receipt) sdkgo.MutationAttempt[UpdateTicketOutput] {
	return sdkgo.NewMutationUncertain(value, zendeskFailure(kind, updateTicketOperation,
		"Zendesk accepted an update by this Step that could not be confirmed: "+reason), receipt)
}

// updateTicketBranchForExchange returns the branch of a non-success outcome that no earlier send can have caused.
func updateTicketBranchForExchange(result zendeskExchange, receipt sdkgo.Receipt, value UpdateTicketOutput) sdkgo.MutationAttempt[UpdateTicketOutput] {
	switch result.outcome {
	case exchangeNotFound:
		return sdkgo.NewMutationBranch(UpdateTicketBranchNotFound, value, &result.failure, receipt)
	case exchangeInvalid:
		return sdkgo.NewMutationBranch(UpdateTicketBranchInvalidResponse, value, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewMutationBranch(UpdateTicketBranchDefect, value, &result.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(UpdateTicketBranchProviderRejected, value, &result.failure, receipt)
	}
}

func validateUpdateTicketInput(input UpdateTicketInput) error {
	if input.TicketID < 1 {
		return errors.New("ticketId must be a positive Zendesk ticket ID")
	}
	if input.Status != "" {
		if err := validateTicketStatus(input.Status); err != nil {
			return err
		}
	}
	if input.Priority != "" {
		if err := validateTicketPriority(input.Priority); err != nil {
			return err
		}
	}
	if err := errors.Join(validateOptionalID("assigneeId", input.AssigneeID), validateOptionalID("groupId", input.GroupID),
		validateTags("addTags", input.AddTags), validateTags("removeTags", input.RemoveTags)); err != nil {
		return err
	}
	for _, tag := range input.AddTags {
		if slices.Contains(input.RemoveTags, tag) {
			return fmt.Errorf("tag %q is in both addTags and removeTags", tag)
		}
	}
	if input.Comment != nil {
		if err := validateCommentInput("comment", *input.Comment); err != nil {
			return err
		}
	}
	if input.ExpectedUpdatedAt != "" {
		if _, err := time.Parse(time.RFC3339, input.ExpectedUpdatedAt); err != nil {
			return errors.New("expectedUpdatedAt must be an RFC 3339 instant such as 2026-01-28T08:45:00Z")
		}
	}
	hasRequestedChange := input.Status != "" || input.Priority != "" || input.AssigneeID != 0 || input.GroupID != 0 ||
		len(input.AddTags) != 0 || len(input.RemoveTags) != 0 || input.Comment != nil
	if !hasRequestedChange {
		return errors.New("at least one of status, priority, assigneeId, groupId, addTags, removeTags, or comment is required")
	}
	return nil
}

// planTicketChange sends only fields that differ from the ticket as read; no difference means already applied.
func planTicketChange(input UpdateTicketInput, current decodedTicket, idempotencyKey sdkgo.IdempotencyKey) (updateTicketWire, bool) {
	ticket := current.ticket
	change := updateTicketWire{
		Metadata: map[string]string{idempotencyMetadataKey: string(idempotencyKey)}, SafeUpdate: true, UpdatedStamp: current.updatedStamp,
	}
	hasEffect := false
	if input.Status != "" && input.Status != ticket.Status {
		change.Status, hasEffect = string(input.Status), true
	}
	if input.Priority != "" && input.Priority != ticket.Priority {
		change.Priority, hasEffect = string(input.Priority), true
	}
	if input.AssigneeID != 0 && input.AssigneeID != ticket.AssigneeID {
		change.AssigneeID, hasEffect = input.AssigneeID, true
	}
	if input.GroupID != 0 && input.GroupID != ticket.GroupID {
		change.GroupID, hasEffect = input.GroupID, true
	}
	tags := make([]string, 0, len(ticket.Tags)+len(input.AddTags))
	for _, tag := range ticket.Tags {
		if !slices.Contains(input.RemoveTags, tag) {
			tags = append(tags, tag)
		}
	}
	for _, tag := range input.AddTags {
		if !slices.Contains(tags, tag) {
			tags = append(tags, tag)
		}
	}
	if !slices.Equal(tags, ticket.Tags) {
		change.Tags, hasEffect = &tags, true
	}
	if input.Comment != nil {
		change.Comment, hasEffect = &ticketCommentWire{Body: input.Comment.Body, Public: !input.Comment.IsInternalNote}, true
	}
	return change, hasEffect
}

// hasAuditWithIdempotencyKey scans one page of the newest audits for this call's metadata entry.
func hasAuditWithIdempotencyKey(body []byte, idempotencyKey sdkgo.IdempotencyKey) (bool, error) {
	var page struct {
		Audits []struct {
			Metadata struct {
				Custom map[string]json.RawMessage `json:"custom"`
			} `json:"metadata"`
		} `json:"audits"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return false, errors.New("audit page is not JSON")
	}
	if page.Audits == nil {
		return false, errors.New("audit page has no audits array")
	}
	for _, audit := range page.Audits {
		var recordedKey string
		if json.Unmarshal(audit.Metadata.Custom[idempotencyMetadataKey], &recordedKey) == nil && recordedKey == string(idempotencyKey) {
			return true, nil
		}
	}
	return false, nil
}
