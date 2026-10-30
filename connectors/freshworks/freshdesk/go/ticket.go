// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// TicketStatus is Freshdesk's own integer ticket status. Freshdesk fixes 2 through 5 and lets
// an administrator add custom statuses under Admin > Workflows > Ticket Fields, so an account
// can report other values, which Results pass through unchanged. GET /api/v2/ticket_fields
// lists the account's statuses as the choices of its default_status field.
type TicketStatus int

const (
	// TicketStatusOpen is Freshdesk's Open status (2): the ticket needs an agent. Freshdesk
	// creates tickets as Open and reopens a ticket when the requester replies.
	TicketStatusOpen TicketStatus = 2
	// TicketStatusPending is Freshdesk's Pending status (3): the agent is waiting for more
	// information, and SLA timers pause.
	TicketStatusPending TicketStatus = 3
	// TicketStatusResolved is Freshdesk's Resolved status (4): an agent provided a solution.
	TicketStatusResolved TicketStatus = 4
	// TicketStatusClosed is Freshdesk's Closed status (5): the requester's side is done.
	TicketStatusClosed TicketStatus = 5
	// TicketStatusWaitingOnCustomer is the custom status 6 that Freshdesk's API documentation
	// shows as Waiting on Customer. It exists only in accounts that define it.
	TicketStatusWaitingOnCustomer TicketStatus = 6
	// TicketStatusWaitingOnThirdParty is the custom status 7 that Freshdesk's API documentation
	// shows as Waiting on Third Party. It exists only in accounts that define it.
	TicketStatusWaitingOnThirdParty TicketStatus = 7
)

// FixedTicketStatuses returns the four statuses every Freshdesk account has and cannot delete,
// in Freshdesk's order: Open (2), Pending (3), Resolved (4), and Closed (5).
func FixedTicketStatuses() []TicketStatus {
	return []TicketStatus{TicketStatusOpen, TicketStatusPending, TicketStatusResolved, TicketStatusClosed}
}

// TicketPriority is Freshdesk's own integer ticket priority. Freshdesk's four priorities are
// built in and cannot be edited.
type TicketPriority int

const (
	// TicketPriorityLow is Freshdesk's Low priority (1), the default for a new ticket.
	TicketPriorityLow TicketPriority = 1
	// TicketPriorityMedium is Freshdesk's Medium priority (2).
	TicketPriorityMedium TicketPriority = 2
	// TicketPriorityHigh is Freshdesk's High priority (3).
	TicketPriorityHigh TicketPriority = 3
	// TicketPriorityUrgent is Freshdesk's Urgent priority (4).
	TicketPriorityUrgent TicketPriority = 4
)

// TicketPriorities returns Freshdesk's four priorities in Freshdesk's order, Low (1) to Urgent (4).
func TicketPriorities() []TicketPriority {
	return []TicketPriority{TicketPriorityLow, TicketPriorityMedium, TicketPriorityHigh, TicketPriorityUrgent}
}

// TicketSource is Freshdesk's own integer for the channel that created a ticket. Results pass
// through values Freshdesk does not document here, such as social channels.
type TicketSource int

const (
	// TicketSourceEmail is a ticket created from an email (1).
	TicketSourceEmail TicketSource = 1
	// TicketSourcePortal is a ticket created in the support portal (2), Freshdesk's default for API-created tickets.
	TicketSourcePortal TicketSource = 2
	// TicketSourcePhone is a ticket created from a phone call (3).
	TicketSourcePhone TicketSource = 3
	// TicketSourceChat is a ticket created from chat (7).
	TicketSourceChat TicketSource = 7
	// TicketSourceFeedbackWidget is a ticket created from the feedback widget (9).
	TicketSourceFeedbackWidget TicketSource = 9
	// TicketSourceOutboundEmail is a ticket an agent started as an outbound email (10).
	TicketSourceOutboundEmail TicketSource = 10
)

// ConversationSource is Freshdesk's own integer for the kind of a ticket conversation. Results
// pass through the other documented values, such as 5 for tweets and 9 for phone.
type ConversationSource int

const (
	// ConversationSourceReply is a reply (0), which Freshdesk emails to the requester.
	ConversationSourceReply ConversationSource = 0
	// ConversationSourceNote is a note (2), private or public.
	ConversationSourceNote ConversationSource = 2
)

// Ticket is the connector's view of one Freshdesk ticket. Custom fields, email addresses,
// attachments, and association details are omitted. Zero IDs mean Freshdesk reported null,
// such as an unassigned ticket.
type Ticket struct {
	// ID is the Freshdesk ticket ID.
	ID int64 `json:"id"`
	// Subject is the ticket subject.
	Subject string `json:"subject,omitempty"`
	// Description is the ticket's plain-text content. searchTickets leaves it empty to keep pages small.
	Description string `json:"description,omitempty"`
	// IsDescriptionTruncated reports that Description was cut at MaxTextBytes.
	IsDescriptionTruncated bool `json:"isDescriptionTruncated,omitempty"`
	// Status is Freshdesk's integer status.
	Status TicketStatus `json:"status"`
	// Priority is Freshdesk's integer priority.
	Priority TicketPriority `json:"priority"`
	// Source is the channel that created the ticket.
	Source TicketSource `json:"source,omitempty"`
	// Type is the account's ticket type label, such as Question or Problem, or empty for none.
	Type string `json:"type,omitempty"`
	// RequesterID is the contact who asked for support.
	RequesterID int64 `json:"requesterId,omitempty"`
	// ResponderID is the assigned agent, Freshdesk's responder_id, or zero when unassigned.
	ResponderID int64 `json:"responderId,omitempty"`
	// GroupID is the assigned group, or zero.
	GroupID int64 `json:"groupId,omitempty"`
	// CompanyID is the requester's company for this ticket, or zero.
	CompanyID int64 `json:"companyId,omitempty"`
	// ProductID is the product the ticket belongs to, or zero.
	ProductID int64 `json:"productId,omitempty"`
	// Tags lists the ticket's tags. Freshdesk's search and list responses may omit them, so
	// read the ticket with getTicket before relying on its tags.
	Tags []string `json:"tags,omitempty"`
	// IsEscalated reports that Freshdesk escalated the ticket for any reason.
	IsEscalated bool `json:"isEscalated,omitempty"`
	// IsSpam reports that the ticket is marked as spam; Freshdesk rejects updates to it.
	IsSpam bool `json:"isSpam,omitempty"`
	// CreatedAt is when the ticket was created.
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is when the ticket last changed, at Freshdesk's one-second precision.
	UpdatedAt time.Time `json:"updatedAt"`
	// DueBy is when the ticket is due to be resolved, or nil.
	DueBy *time.Time `json:"dueBy,omitempty"`
	// FirstResponseDueBy is when the first response is due, or nil.
	FirstResponseDueBy *time.Time `json:"firstResponseDueBy,omitempty"`
}

// TicketRequester is the connector's view of the contact who requested a ticket.
type TicketRequester struct {
	// ID is the Freshdesk contact ID.
	ID int64 `json:"id"`
	// Name is the contact's name.
	Name string `json:"name,omitempty"`
	// Email is the contact's primary email address, or empty when Freshdesk has none.
	Email string `json:"email,omitempty"`
}

// TicketConversation is one reply or note on a ticket.
type TicketConversation struct {
	// ID is the Freshdesk conversation ID.
	ID int64 `json:"id"`
	// TicketID is the ticket the conversation belongs to.
	TicketID int64 `json:"ticketId,omitempty"`
	// UserID is the agent or contact who added the conversation.
	UserID int64 `json:"userId,omitempty"`
	// Source is the kind of conversation, such as ConversationSourceReply or ConversationSourceNote.
	Source ConversationSource `json:"source"`
	// IsPrivate is true for a private note that only agents can see.
	IsPrivate bool `json:"isPrivate"`
	// IsIncoming is true for a conversation that appears as created from outside the portal,
	// such as a requester's emailed reply.
	IsIncoming bool `json:"isIncoming,omitempty"`
	// Body is the conversation as plain text.
	Body string `json:"body"`
	// IsBodyTruncated reports that Body was cut at MaxTextBytes.
	IsBodyTruncated bool `json:"isBodyTruncated,omitempty"`
	// CreatedAt is when the conversation was added.
	CreatedAt time.Time `json:"createdAt"`
}

// MaxTextBytes bounds each ticket description and conversation body a Result carries, so
// durable Step state stays small. Longer text is cut at a UTF-8 boundary and flagged.
const MaxTextBytes = 16 << 10

const maximumTicketTypeLength = 255

// tagPattern accepts tags that are safe inside a single-quoted filter-query value.
var tagPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9 _./+-]{0,62}[A-Za-z0-9_./+-])?$`)

// freshdeskTicketWire is the ticket JSON the connector reads; Freshdesk's nulls decode as zero values.
type freshdeskTicketWire struct {
	ID              int64    `json:"id"`
	Subject         string   `json:"subject"`
	DescriptionText string   `json:"description_text"`
	Status          int      `json:"status"`
	Priority        int      `json:"priority"`
	Source          int      `json:"source"`
	Type            string   `json:"type"`
	RequesterID     int64    `json:"requester_id"`
	ResponderID     int64    `json:"responder_id"`
	GroupID         int64    `json:"group_id"`
	CompanyID       int64    `json:"company_id"`
	ProductID       int64    `json:"product_id"`
	Tags            []string `json:"tags"`
	IsEscalated     bool     `json:"is_escalated"`
	Spam            bool     `json:"spam"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
	DueBy           string   `json:"due_by"`
	FRDueBy         string   `json:"fr_due_by"`
}

type freshdeskRequesterWire struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type freshdeskConversationWire struct {
	ID        int64  `json:"id"`
	TicketID  int64  `json:"ticket_id"`
	UserID    int64  `json:"user_id"`
	Source    int    `json:"source"`
	Private   bool   `json:"private"`
	Incoming  bool   `json:"incoming"`
	BodyText  string `json:"body_text"`
	CreatedAt string `json:"created_at"`
}

// decodeTicketWire validates one ticket and converts Freshdesk's snake_case fields.
func decodeTicketWire(wire freshdeskTicketWire) (Ticket, error) {
	if wire.ID < 1 {
		return Ticket{}, errors.New("ticket ID is missing")
	}
	if wire.Status < 1 {
		return Ticket{}, errors.New("ticket status is missing")
	}
	createdAt, err := time.Parse(time.RFC3339, wire.CreatedAt)
	if err != nil {
		return Ticket{}, errors.New("ticket created_at is not RFC 3339")
	}
	updatedAt, err := time.Parse(time.RFC3339, wire.UpdatedAt)
	if err != nil {
		return Ticket{}, errors.New("ticket updated_at is not RFC 3339")
	}
	description, isDescriptionTruncated := truncateUTF8(wire.DescriptionText, MaxTextBytes)
	ticket := Ticket{
		ID: wire.ID, Subject: wire.Subject, Description: description, IsDescriptionTruncated: isDescriptionTruncated,
		Status: TicketStatus(wire.Status), Priority: TicketPriority(wire.Priority), Source: TicketSource(wire.Source),
		Type: wire.Type, RequesterID: wire.RequesterID, ResponderID: wire.ResponderID, GroupID: wire.GroupID,
		CompanyID: wire.CompanyID, ProductID: wire.ProductID, Tags: wire.Tags, IsEscalated: wire.IsEscalated,
		IsSpam: wire.Spam, CreatedAt: createdAt.UTC(), UpdatedAt: updatedAt.UTC(),
	}
	if ticket.DueBy, err = parseOptionalTimestamp("due_by", wire.DueBy); err != nil {
		return Ticket{}, err
	}
	if ticket.FirstResponseDueBy, err = parseOptionalTimestamp("fr_due_by", wire.FRDueBy); err != nil {
		return Ticket{}, err
	}
	return ticket, nil
}

// decodeTicketBody decodes the bare ticket object Freshdesk returns from ticket reads and writes.
func decodeTicketBody(body []byte, expectedTicketID int64) (Ticket, error) {
	var wire freshdeskTicketWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return Ticket{}, errors.New("ticket response is not a ticket object")
	}
	ticket, err := decodeTicketWire(wire)
	if err != nil {
		return Ticket{}, err
	}
	if expectedTicketID > 0 && ticket.ID != expectedTicketID {
		return Ticket{}, errors.New("ticket response is for another ticket")
	}
	return ticket, nil
}

func decodeConversationWire(wire freshdeskConversationWire) (TicketConversation, error) {
	if wire.ID < 1 {
		return TicketConversation{}, errors.New("conversation ID is missing")
	}
	createdAt, err := time.Parse(time.RFC3339, wire.CreatedAt)
	if err != nil {
		return TicketConversation{}, errors.New("conversation created_at is not RFC 3339")
	}
	body, isBodyTruncated := truncateUTF8(wire.BodyText, MaxTextBytes)
	return TicketConversation{
		ID: wire.ID, TicketID: wire.TicketID, UserID: wire.UserID, Source: ConversationSource(wire.Source),
		IsPrivate: wire.Private, IsIncoming: wire.Incoming, Body: body, IsBodyTruncated: isBodyTruncated,
		CreatedAt: createdAt.UTC(),
	}, nil
}

func parseOptionalTimestamp(fieldName string, value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	instant, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("ticket %s is not RFC 3339", fieldName)
	}
	instant = instant.UTC()
	return &instant, nil
}

// plainTextToFreshdeskHTML escapes text for Freshdesk's HTML fields, keeping line breaks as <br>.
func plainTextToFreshdeskHTML(text string) string {
	escaped := html.EscapeString(strings.ReplaceAll(text, "\r\n", "\n"))
	return strings.ReplaceAll(escaped, "\n", "<br>")
}

func truncateUTF8(value string, maxBytes int) (string, bool) {
	if len(value) <= maxBytes {
		return value, false
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut], true
}

// validateTicketStatus accepts any value from Freshdesk's lowest status up, because custom statuses vary by account.
func validateTicketStatus(fieldName string, status TicketStatus) error {
	if status < TicketStatusOpen {
		return fmt.Errorf("%s %d is not a Freshdesk status; use 2 (Open), 3 (Pending), 4 (Resolved), 5 (Closed), or a custom status value of the account", fieldName, status)
	}
	return nil
}

func validateTicketPriority(fieldName string, priority TicketPriority) error {
	if priority < TicketPriorityLow || priority > TicketPriorityUrgent {
		return fmt.Errorf("%s %d is not a Freshdesk priority; use 1 (Low), 2 (Medium), 3 (High), or 4 (Urgent)", fieldName, priority)
	}
	return nil
}

func validateTags(fieldName string, tags []string) error {
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if !tagPattern.MatchString(tag) {
			return fmt.Errorf("%s entry %q must be 1 to 64 letters, digits, spaces, and _ . / + - characters, starting and ending with a letter, digit, or symbol", fieldName, tag)
		}
		folded := strings.ToLower(tag)
		if seen[folded] {
			return fmt.Errorf("%s lists %q twice", fieldName, tag)
		}
		seen[folded] = true
	}
	return nil
}

func validateTicketType(ticketType string) error {
	if ticketType != strings.TrimSpace(ticketType) || len(ticketType) > maximumTicketTypeLength || !utf8.ValidString(ticketType) {
		return fmt.Errorf("type must be one of the account's ticket type labels, such as Question, without surrounding spaces")
	}
	return nil
}

func validateTextInput(fieldName string, text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("%s is required", fieldName)
	}
	if !utf8.ValidString(text) {
		return fmt.Errorf("%s must be UTF-8 text", fieldName)
	}
	return nil
}

func validateOptionalID(fieldName string, id int64) error {
	if id < 0 {
		return fmt.Errorf("%s cannot be negative", fieldName)
	}
	return nil
}
