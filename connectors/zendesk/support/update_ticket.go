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
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
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

// Invoke reads the ticket, looks for an audit carrying this call's key when a comment or
// expectedUpdatedAt makes a repeat observable, and otherwise writes the change as a Zendesk
// safe update against the updated_at it read. A concurrent write, including a second dispatch
// of this Step, returns 409, and the retried attempt rereads before writing again.
func (operation UpdateTicketOperation) Invoke(call sdkgo.Call, input UpdateTicketInput) sdkgo.MutationAttempt[UpdateTicketOutput] {
	if err := validateUpdateTicketInput(input); err != nil {
		return sdkgo.NewMutationBranch(UpdateTicketBranchDefect, UpdateTicketOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, updateTicketOperation, err.Error()), sdkgo.Receipt{})
	}
	if call.IdempotencyKey == "" {
		return sdkgo.NewMutationBranch(UpdateTicketBranchDefect, UpdateTicketOutput{}, zendeskFailurePointer(sdkgo.FailureLocalDefect, updateTicketOperation, "connector call has no idempotency key"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, updateTicketOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(UpdateTicketBranchDefect, UpdateTicketOutput{}, failure, sdkgo.Receipt{})
	}
	readResult := operation.client.exchange(call, credentials, updateTicketOperation, zendeskRequest{method: http.MethodGet, path: ticketPath(input.TicketID)})
	receipt := operation.client.receipt(call, readResult.response, input.TicketID)
	if attempt, isTerminal := updateTicketAttemptForExchange(readResult, receipt, UpdateTicketOutput{}); isTerminal {
		return attempt
	}
	current, err := decodeTicketEnvelope(readResult.response.body, operation.client.agentTicketURL)
	if err == nil && current.ticket.ID != input.TicketID {
		err = errors.New("ticket response is for another ticket")
	}
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateTicketBranchInvalidResponse, UpdateTicketOutput{}, zendeskFailurePointer(sdkgo.FailureProtocol, updateTicketOperation, "Zendesk returned an invalid ticket: "+err.Error()), receipt)
	}
	currentOutput := UpdateTicketOutput{Ticket: current.ticket}
	if input.Comment != nil || input.ExpectedUpdatedAt != "" {
		auditResult := operation.client.exchange(call, credentials, updateTicketOperation, zendeskRequest{
			method: http.MethodGet, path: ticketPath(input.TicketID) + "/audits", query: url.Values{"sort_order": {"desc"}},
		})
		receipt = operation.client.receipt(call, auditResult.response, input.TicketID)
		if attempt, isTerminal := updateTicketAttemptForExchange(auditResult, receipt, currentOutput); isTerminal {
			return attempt
		}
		hasEarlierChange, err := hasAuditWithIdempotencyKey(auditResult.response.body, call.IdempotencyKey)
		if err != nil {
			return sdkgo.NewMutationBranch(UpdateTicketBranchInvalidResponse, currentOutput, zendeskFailurePointer(sdkgo.FailureProtocol, updateTicketOperation, "Zendesk returned an invalid audit page: "+err.Error()), receipt)
		}
		if hasEarlierChange {
			return sdkgo.NewMutationBranch(UpdateTicketBranchUpdated, UpdateTicketOutput{Ticket: current.ticket, WasAlreadyApplied: true}, nil, receipt)
		}
	}
	if input.ExpectedUpdatedAt != "" {
		// validateUpdateTicketInput already rejected an unparsable value.
		expectedUpdatedAt, _ := time.Parse(time.RFC3339, input.ExpectedUpdatedAt)
		if !expectedUpdatedAt.Equal(current.ticket.UpdatedAt) {
			return sdkgo.NewMutationBranch(UpdateTicketBranchConflict, currentOutput, zendeskFailurePointer(sdkgo.FailureConflict, updateTicketOperation,
				"ticket was updated at "+current.ticket.UpdatedAt.Format(time.RFC3339)+", after expectedUpdatedAt"), receipt)
		}
	}
	change, hasEffect := planTicketChange(input, current, call.IdempotencyKey)
	if !hasEffect {
		return sdkgo.NewMutationBranch(UpdateTicketBranchUpdated, UpdateTicketOutput{Ticket: current.ticket, WasAlreadyApplied: true}, nil, receipt)
	}
	writeResult := operation.client.exchange(call, credentials, updateTicketOperation, zendeskRequest{
		method: http.MethodPut, path: ticketPath(input.TicketID), payload: updateTicketRequestWire{Ticket: change},
	})
	receipt = operation.client.receipt(call, writeResult.response, input.TicketID)
	if attempt, isTerminal := updateTicketAttemptForExchange(writeResult, receipt, currentOutput); isTerminal {
		return attempt
	}
	updated, err := decodeTicketEnvelope(writeResult.response.body, operation.client.agentTicketURL)
	if err == nil && updated.ticket.ID != input.TicketID {
		err = errors.New("ticket response is for another ticket")
	}
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateTicketBranchInvalidResponse, currentOutput, zendeskFailurePointer(sdkgo.FailureProtocol, updateTicketOperation, "Zendesk returned an invalid updated ticket: "+err.Error()), receipt)
	}
	return sdkgo.NewMutationBranch(UpdateTicketBranchUpdated, UpdateTicketOutput{Ticket: updated.ticket}, nil, receipt)
}

// updateTicketAttemptForExchange returns the terminal attempt for every outcome except success.
func updateTicketAttemptForExchange(result zendeskExchange, receipt sdkgo.Receipt, value UpdateTicketOutput) (sdkgo.MutationAttempt[UpdateTicketOutput], bool) {
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[UpdateTicketOutput]{}, false
	case exchangeRetryable:
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
