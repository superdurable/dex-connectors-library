// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// TicketChangeKind is the derived kind of one ticketChanged delivery. Zoho Desk ticket search reports no
// change type, so the connector derives it from the poll cursor and the ticket's created time.
type TicketChangeKind string

const (
	// TicketChangeCreated is a ticket the Trigger had not seen before and that was created inside the
	// window the poll read, so it is new since the previous poll. Changes made to a new ticket before the
	// poll read it, such as an assignment rule, arrive inside this one delivery.
	TicketChangeCreated TicketChangeKind = "created"
	// TicketChangeUpdated is any other change: a status, priority, assignee, department, or field change,
	// a new thread or comment that moves the ticket's modified time, or a ticket created before the baseline.
	TicketChangeUpdated TicketChangeKind = "updated"
)

// TicketChangedTriggerConfiguration is the ticketChanged binding configuration that the department picker
// saves in Dex Web. Changing the department of a binding that already polled makes its next poll record a
// new baseline for the new department.
type TicketChangedTriggerConfiguration struct {
	// DepartmentID is the watched department's numeric Zoho Desk ID from the department picker, such as
	// 1892000000006907; blank watches every department the connection's agent can see.
	DepartmentID string `json:"departmentId,omitempty"`
	// DepartmentName is the picked department's name, kept for display only.
	DepartmentName string `json:"departmentName,omitempty"`
}

// Validate rejects a department ID that is not a numeric Zoho Desk ID; blank is valid.
func (configuration TicketChangedTriggerConfiguration) Validate() error {
	return validateOptionalZohoID("ticketChanged departmentId", configuration.DepartmentID)
}

// TicketChangedEvent is one created or changed ticket read from Zoho Desk ticket search. Several changes
// to one ticket between two polls arrive as one delivery of its latest version. Tickets moved to the
// trash, deleted, or archived leave the search results and are not delivered.
type TicketChangedEvent struct {
	// OrgID is the connection's Zoho Desk organization.
	OrgID string `json:"orgId"`
	// DepartmentID is the watched department, or empty when the binding watches every department.
	DepartmentID string `json:"departmentId,omitempty"`
	// ChangeKind is the derived change kind; see TicketChangeKind.
	ChangeKind TicketChangeKind `json:"changeKind"`
	// Ticket is the ticket as the search returned it, without its DescriptionHTML. Read it with getTicket
	// for the description, threads, and comments.
	Ticket Ticket `json:"ticket"`
}

// newTicketChangedTriggerEvent converts one search result. Its ID is a pure function of the ticket
// version, so a refetch after a crash yields the same ID.
func newTicketChangedTriggerEvent(organizationID string, departmentID string, kind TicketChangeKind, ticket Ticket) sdkgo.TriggerEvent[TicketChangedEvent] {
	return sdkgo.TriggerEvent[TicketChangedEvent]{
		ID:         ticketChangeID(organizationID, ticket),
		OccurredAt: ticket.ModifiedAt.UTC(),
		Payload:    TicketChangedEvent{OrgID: organizationID, DepartmentID: departmentID, ChangeKind: kind, Ticket: ticket},
	}
}

// ticketChangeID hashes the organization, ticket, and modified time, so it stays short for any ID length.
func ticketChangeID(organizationID string, ticket Ticket) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{organizationID, ticket.ID, formatZohoTimestamp(ticket.ModifiedAt)}, "\x00")))
	return "zdesk-" + hex.EncodeToString(digest[:16])
}

// ticketVersionHash is the keyed-snapshot hash of one ticket version.
func ticketVersionHash(ticket Ticket) string {
	digest := sha256.Sum256([]byte(ticket.ID + "\x00" + formatZohoTimestamp(ticket.ModifiedAt)))
	return hex.EncodeToString(digest[:16])
}

// ticketChangeKind reports created for a ticket absent from the snapshot whose created time is inside the
// window the sweep began at; isKnown reports that the snapshot already holds an earlier version.
func ticketChangeKind(ticket Ticket, isKnown bool, windowStart time.Time) TicketChangeKind {
	if !isKnown && !ticket.CreatedAt.Before(windowStart) {
		return TicketChangeCreated
	}
	return TicketChangeUpdated
}

func ticketChangedTriggerConfigurationError(err error) error {
	return fmt.Errorf("zoho-desk ticketChanged configuration: %w", err)
}

var errTicketCursorUndecodable = errors.New("Zoho Desk ticket cursor is not decodable")
