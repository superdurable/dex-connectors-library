// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// TicketChangeKind is the derived kind of one ticketChanged delivery. Freshdesk's ticket list reports no
// change type, so the kind is derived from the ticket's created_at and the binding's committed cursor.
type TicketChangeKind string

const (
	// TicketChangeCreated is a ticket created inside the current poll window (TicketChangeOverlap before
	// the binding's committed updated_at high-water mark, or later) that the binding has not delivered before. Edits made before the next poll arrive within the same delivery.
	TicketChangeCreated TicketChangeKind = "created"
	// TicketChangeUpdated is any later change that moved the ticket's updated_at, such as a status,
	// priority, assignment, or tag change, or a new reply or note.
	TicketChangeUpdated TicketChangeKind = "updated"
)

// TicketChangedTriggerConfiguration is the ticketChanged binding configuration saved for a Trigger
// binding in the project configuration. Freshdesk setup commands cannot authenticate with the agent's
// HTTP Basic API key, so the binding has no provider picker; every field is optional.
type TicketChangedTriggerConfiguration struct {
	// ChangeKinds limits deliveries to these kinds, such as ["created"] to start a Flow only for new
	// tickets. Empty, the default, delivers both created and updated tickets. Skipped changes still move
	// the cursor, so adding a kind later never delivers changes the binding already passed.
	ChangeKinds []TicketChangeKind `json:"changeKinds,omitempty"`
}

// Validate rejects an unknown or repeated change kind; an empty configuration is valid.
func (configuration TicketChangedTriggerConfiguration) Validate() error {
	seen := map[TicketChangeKind]bool{}
	for _, kind := range configuration.ChangeKinds {
		if kind != TicketChangeCreated && kind != TicketChangeUpdated {
			return fmt.Errorf("ticketChanged changeKinds entry %q must be created or updated", kind)
		}
		if seen[kind] {
			return fmt.Errorf("ticketChanged changeKinds lists %q twice", kind)
		}
		seen[kind] = true
	}
	return nil
}

// DeliversChangeKind reports whether the binding delivers kind: every kind when ChangeKinds is empty.
func (configuration TicketChangedTriggerConfiguration) DeliversChangeKind(kind TicketChangeKind) bool {
	if len(configuration.ChangeKinds) == 0 {
		return true
	}
	for _, configured := range configuration.ChangeKinds {
		if configured == kind {
			return true
		}
	}
	return false
}

// TicketChangedEvent is one created or updated ticket read from Freshdesk's List All Tickets endpoint.
// Several changes made between two polls arrive as one delivery of the ticket's latest version. Spam and
// deleted tickets are not listed, so a ticket moved to spam or trash is not delivered.
type TicketChangedEvent struct {
	// ChangeKind is the derived change kind; see TicketChangeKind.
	ChangeKind TicketChangeKind `json:"changeKind"`
	// Ticket is the ticket as the list returned it. Description is empty, because the list omits it
	// unless a request pays extra API credits; read the ticket with getTicket for its text and conversations.
	Ticket Ticket `json:"ticket"`
}

// newTicketChangedTriggerEvent converts one listed ticket. Its ID is a pure function of the ticket ID and
// updated_at, so a refetch after a crash yields the same ID and a later change yields a new one.
func newTicketChangedTriggerEvent(kind TicketChangeKind, ticket Ticket) sdkgo.TriggerEvent[TicketChangedEvent] {
	return sdkgo.TriggerEvent[TicketChangedEvent]{
		ID:         ticketChangeKey(ticket),
		OccurredAt: ticket.UpdatedAt,
		Payload:    TicketChangedEvent{ChangeKind: kind, Ticket: ticket},
	}
}

// ticketChangeKey names one ticket version as "freshdesk-ticket-<id>-<updated_at Unix seconds>".
func ticketChangeKey(ticket Ticket) string {
	return "freshdesk-ticket-" + strconv.FormatInt(ticket.ID, 10) + "-" + strconv.FormatInt(ticket.UpdatedAt.Unix(), 10)
}

func ticketChangedTriggerConfigurationError(err error) error {
	return fmt.Errorf("freshdesk ticketChanged configuration: %w", err)
}

var errTicketChangeCursorInvalid = errors.New("Freshdesk ticketChanged cursor is not decodable")
