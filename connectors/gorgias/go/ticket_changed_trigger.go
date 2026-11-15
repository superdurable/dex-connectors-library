// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias

import (
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// TicketEventType is a Gorgias event type whose object is a ticket, spelled as Gorgias's event log spells it.
type TicketEventType string

// The ticket event types ticketChanged reads. Message events (ticket-message-created and the like) are
// not among them: their object is a message, and Gorgias documents no ticket ID for it.
const (
	TicketEventCreated           TicketEventType = "ticket-created"
	TicketEventUpdated           TicketEventType = "ticket-updated"
	TicketEventClosed            TicketEventType = "ticket-closed"
	TicketEventReopened          TicketEventType = "ticket-reopened"
	TicketEventAssigned          TicketEventType = "ticket-assigned"
	TicketEventUnassigned        TicketEventType = "ticket-unassigned"
	TicketEventTeamAssigned      TicketEventType = "ticket-team-assigned"
	TicketEventTeamUnassigned    TicketEventType = "ticket-team-unassigned"
	TicketEventTagsAdded         TicketEventType = "ticket-tags-added"
	TicketEventTagsRemoved       TicketEventType = "ticket-tags-removed"
	TicketEventSubjectUpdated    TicketEventType = "ticket-subject-updated"
	TicketEventCustomerUpdated   TicketEventType = "ticket-customer-updated"
	TicketEventSnoozed           TicketEventType = "ticket-snoozed"
	TicketEventSelfUnsnoozed     TicketEventType = "ticket-self-unsnoozed"
	TicketEventMarkedSpam        TicketEventType = "ticket-marked-spam"
	TicketEventUnmarkedSpam      TicketEventType = "ticket-unmarked-spam"
	TicketEventTrashed           TicketEventType = "ticket-trashed"
	TicketEventUntrashed         TicketEventType = "ticket-untrashed"
	TicketEventDeleted           TicketEventType = "ticket-deleted"
	TicketEventMerged            TicketEventType = "ticket-merged"
	TicketEventSplit             TicketEventType = "ticket-split"
	TicketEventSLAPolicyAssigned TicketEventType = "ticket-sla-policy-assigned"
)

var ticketEventTypes = []TicketEventType{
	TicketEventCreated, TicketEventUpdated, TicketEventClosed, TicketEventReopened,
	TicketEventAssigned, TicketEventUnassigned, TicketEventTeamAssigned, TicketEventTeamUnassigned,
	TicketEventTagsAdded, TicketEventTagsRemoved, TicketEventSubjectUpdated, TicketEventCustomerUpdated,
	TicketEventSnoozed, TicketEventSelfUnsnoozed, TicketEventMarkedSpam, TicketEventUnmarkedSpam,
	TicketEventTrashed, TicketEventUntrashed, TicketEventDeleted, TicketEventMerged, TicketEventSplit,
	TicketEventSLAPolicyAssigned,
}

// TicketEventTypes returns every event type ticketChanged reads, in the order each poll requests them.
func TicketEventTypes() []TicketEventType { return slices.Clone(ticketEventTypes) }

// TicketChangedTriggerConfiguration is the ticketChanged binding configuration saved for a Trigger
// binding in the project configuration. Studio setup commands cannot send a Gorgias API key over HTTP
// Basic to a per-account host, so the binding has no picker; every field is optional.
type TicketChangedTriggerConfiguration struct {
	// EventTypes limits deliveries to these Gorgias event types, such as ["ticket-created"] to start a Flow
	// only for new tickets, or ["ticket-closed"] for a satisfaction follow-up. Each must be one of
	// TicketEventTypes. Empty, the default, delivers every one of them. Skipped events still move the
	// cursor, so adding a type later never delivers events the binding already passed.
	EventTypes []TicketEventType `json:"eventTypes,omitempty"`
}

// Validate rejects an unknown or repeated event type; an empty configuration is valid.
func (configuration TicketChangedTriggerConfiguration) Validate() error {
	seen := map[TicketEventType]bool{}
	for _, eventType := range configuration.EventTypes {
		if !slices.Contains(ticketEventTypes, eventType) {
			return fmt.Errorf("ticketChanged eventTypes entry %q is not a ticket event type that ticketChanged reads", eventType)
		}
		if seen[eventType] {
			return fmt.Errorf("ticketChanged eventTypes lists %q twice", eventType)
		}
		seen[eventType] = true
	}
	return nil
}

// DeliversEventType reports whether the binding delivers eventType: every read type when EventTypes is empty.
func (configuration TicketChangedTriggerConfiguration) DeliversEventType(eventType TicketEventType) bool {
	if len(configuration.EventTypes) == 0 {
		return slices.Contains(ticketEventTypes, eventType)
	}
	return slices.Contains(configuration.EventTypes, eventType)
}

// TicketChangedEvent is one Gorgias ticket event read from the account's event log, GET /api/events. It
// names the ticket and what happened, not the ticket's fields: read the ticket with getTicket for its
// current status, tags, customer, and messages. One action in Gorgias can record several events, such as
// ticket-updated and ticket-tags-added, and each arrives as its own delivery.
type TicketChangedEvent struct {
	// EventID is Gorgias's event ID, which also names the Trigger event.
	EventID int64 `json:"eventId"`
	// EventType is the Gorgias event type, such as ticket-created.
	EventType TicketEventType `json:"eventType"`
	// TicketID is the ticket the event is about, the event's object_id.
	TicketID int64 `json:"ticketId"`
	// OccurredAt is the event's created_datetime, read as UTC.
	OccurredAt time.Time `json:"occurredAt"`
	// UserID is the Gorgias user who caused the event; zero when Gorgias recorded none, such as for a rule.
	UserID int64 `json:"userId,omitempty"`
	// Context groups the events Gorgias recorded for one chain of actions, such as a rule's changes.
	Context string `json:"context,omitempty"`
}

// newTicketChangedTriggerEvent converts one event. Its ID is a pure function of Gorgias's event ID, so a
// refetch after a crash yields the same Trigger event ID.
func newTicketChangedTriggerEvent(event TicketChangedEvent) sdkgo.TriggerEvent[TicketChangedEvent] {
	return sdkgo.TriggerEvent[TicketChangedEvent]{ID: ticketEventKey(event.EventID), OccurredAt: event.OccurredAt, Payload: event}
}

// ticketEventKey names one event as "gorgias-event-<id>".
func ticketEventKey(eventID int64) string {
	return "gorgias-event-" + strconv.FormatInt(eventID, 10)
}

func ticketChangedTriggerConfigurationError(err error) error {
	return fmt.Errorf("gorgias ticketChanged configuration: %w", err)
}
