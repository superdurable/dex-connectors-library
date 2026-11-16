// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// createdUpdatedTolerance is how close created_at and updated_at must be for a change to count as a
// creation. Zendesk reports no change type, so the kind is derived from these timestamps.
const createdUpdatedTolerance = time.Second

// TicketChangeKind is the derived kind of one ticketChanged delivery.
type TicketChangeKind string

const (
	// TicketChangeCreated is a new ticket: its updated_at is within one second of its created_at.
	TicketChangeCreated TicketChangeKind = "created"
	// TicketChangeUpdated is any other change to a ticket that is not deleted, such as a reply, a status
	// or assignee change, or an edit made soon after creation but before the next poll read it.
	TicketChangeUpdated TicketChangeKind = "updated"
	// TicketChangeDeleted is a ticket the export reports with status deleted. Bindings with
	// IncludeDeleted false never receive it.
	TicketChangeDeleted TicketChangeKind = "deleted"
)

// TicketChangedTriggerConfiguration is the ticketChanged binding configuration. The Trigger watches the
// whole account, as the connection's administrator sees it; a Flow's accept function narrows deliveries,
// for example to one group or brand. A zero value delivers created and updated tickets only.
type TicketChangedTriggerConfiguration struct {
	// IncludeDeleted also delivers tickets deleted in Zendesk, as TicketChangeDeleted. False sends
	// exclude_deleted=true, so the export omits deletions. Changing it keeps the binding's position.
	IncludeDeleted bool `json:"includeDeleted,omitempty"`
}

// Validate reports a configuration error; every TicketChangedTriggerConfiguration is currently valid.
func (TicketChangedTriggerConfiguration) Validate() error { return nil }

// TicketChangedEvent is one ticket the incremental ticket export reported as created, updated, or
// deleted after the binding's cursor. Several changes made between two polls arrive as one event carrying
// the ticket's latest state.
type TicketChangedEvent struct {
	// ChangeKind is the derived change kind; see TicketChangeKind.
	ChangeKind TicketChangeKind `json:"changeKind"`
	// Ticket is the ticket as the export returned it, with Description cut at MaxTextBytes. A deleted
	// ticket has Status TicketStatusDeleted.
	Ticket Ticket `json:"ticket"`
	// GeneratedAt is Zendesk's generated_timestamp: the last change of the ticket, including system
	// updates that leave UpdatedAt unchanged, at one-second precision. It is zero when Zendesk omits it.
	GeneratedAt time.Time `json:"generatedAt,omitzero"`
}

// newTicketChangedTriggerEvent converts one export ticket. Its ID is a pure function of the ticket version,
// so a refetch after a crash or a cursor reset yields the same ID.
func newTicketChangedTriggerEvent(ticket Ticket, generatedTimestamp int64) sdkgo.TriggerEvent[TicketChangedEvent] {
	event := TicketChangedEvent{ChangeKind: ticketChangeKind(ticket), Ticket: ticket}
	if generatedTimestamp > 0 {
		event.GeneratedAt = time.Unix(generatedTimestamp, 0).UTC()
	}
	occurredAt := ticket.UpdatedAt
	if event.GeneratedAt.After(occurredAt) {
		occurredAt = event.GeneratedAt
	}
	return sdkgo.TriggerEvent[TicketChangedEvent]{
		ID: ticketChangeID(ticket, generatedTimestamp), OccurredAt: occurredAt, Payload: event,
	}
}

// ticketChangeID hashes the ticket identity and version, so it stays short and opaque.
func ticketChangeID(ticket Ticket, generatedTimestamp int64) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		strconv.FormatInt(ticket.ID, 10), strconv.FormatInt(generatedTimestamp, 10),
		ticket.UpdatedAt.Format(time.RFC3339), string(ticket.Status),
	}, "\x00")))
	return "zendesk-ticket-" + hex.EncodeToString(digest[:16])
}

func ticketChangeKind(ticket Ticket) TicketChangeKind {
	if ticket.Status == TicketStatusDeleted {
		return TicketChangeDeleted
	}
	if !ticket.CreatedAt.IsZero() && ticket.UpdatedAt.Sub(ticket.CreatedAt).Abs() <= createdUpdatedTolerance {
		return TicketChangeCreated
	}
	return TicketChangeUpdated
}

func ticketChangedTriggerConfigurationError(err error) error {
	return fmt.Errorf("zendesk-support ticketChanged configuration: %w", err)
}
