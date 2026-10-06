// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias

import (
	"errors"
	"fmt"
	"html"
	"net/mail"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// TicketStatus is Gorgias's own ticket status. Gorgias has exactly two: open and closed. A
// snoozed ticket stays open and carries SnoozedUntil, the time Gorgias reopens it.
type TicketStatus string

const (
	// TicketStatusOpen is a ticket that needs an agent, including a snoozed one.
	TicketStatusOpen TicketStatus = "open"
	// TicketStatusClosed is a ticket an agent or rule closed; a customer reply reopens it.
	TicketStatusClosed TicketStatus = "closed"
)

// TicketStatuses returns Gorgias's two statuses, open then closed.
func TicketStatuses() []TicketStatus {
	return []TicketStatus{TicketStatusOpen, TicketStatusClosed}
}

// TicketPriority is Gorgias's own ticket priority.
type TicketPriority string

const (
	// TicketPriorityLow is Gorgias's low priority.
	TicketPriorityLow TicketPriority = "low"
	// TicketPriorityNormal is Gorgias's default priority for a new ticket.
	TicketPriorityNormal TicketPriority = "normal"
	// TicketPriorityHigh is Gorgias's high priority.
	TicketPriorityHigh TicketPriority = "high"
	// TicketPriorityCritical is Gorgias's highest priority.
	TicketPriorityCritical TicketPriority = "critical"
)

// TicketPriorities returns Gorgias's four priorities from low to critical.
func TicketPriorities() []TicketPriority {
	return []TicketPriority{TicketPriorityLow, TicketPriorityNormal, TicketPriorityHigh, TicketPriorityCritical}
}

const (
	// MessageChannelEmail is the channel of an email message, including a public reply addNote sends.
	MessageChannelEmail = "email"
	// MessageChannelInternalNote is the channel of an internal note, which only agents see.
	MessageChannelInternalNote = "internal-note"
	// MessageChannelAPI is the channel of a message created through the API, such as createTicket's first message.
	MessageChannelAPI = "api"
)

// Ticket is the connector's view of one Gorgias ticket. Custom fields, integrations data,
// events, and satisfaction surveys are omitted. Zero IDs mean Gorgias reported none, such as
// an unassigned ticket.
type Ticket struct {
	// ID is the Gorgias ticket ID.
	ID int64 `json:"id"`
	// Subject is the ticket subject.
	Subject string `json:"subject,omitempty"`
	// Status is Gorgias's status, open or closed, passed through unchanged.
	Status TicketStatus `json:"status"`
	// Priority is Gorgias's priority, or empty when the response carried none.
	Priority TicketPriority `json:"priority,omitempty"`
	// Channel is how the conversation started, such as email, chat, or api.
	Channel string `json:"channel,omitempty"`
	// Via is how the first message was received or sent, such as email or api.
	Via string `json:"via,omitempty"`
	// RequesterID is the Gorgias customer the ticket belongs to.
	RequesterID int64 `json:"requesterId,omitempty"`
	// RequesterEmail is that customer's primary email address, or empty when Gorgias has none.
	RequesterEmail string `json:"requesterEmail,omitempty"`
	// AssigneeUserID is the assigned agent, or zero when unassigned.
	AssigneeUserID int64 `json:"assigneeUserId,omitempty"`
	// AssigneeTeamID is the assigned team, or zero when unassigned.
	AssigneeTeamID int64 `json:"assigneeTeamId,omitempty"`
	// Tags lists the ticket's tag names; Gorgias tag names are case sensitive.
	Tags []string `json:"tags,omitempty"`
	// ExternalID is the ticket's external ID. Tickets createTicket creates carry its idempotency key.
	ExternalID string `json:"externalId,omitempty"`
	// Language is the ISO 639-1 language Gorgias detected or was given, or empty.
	Language string `json:"language,omitempty"`
	// IsSpam reports that the ticket is marked as spam.
	IsSpam bool `json:"isSpam,omitempty"`
	// MessageCount is Gorgias's count of the ticket's messages, reported only by searchTickets.
	MessageCount int `json:"messageCount,omitempty"`
	// CreatedAt is when the ticket was created.
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is when the ticket last changed; it equals CreatedAt when Gorgias reports none.
	UpdatedAt time.Time `json:"updatedAt"`
	// LastReceivedMessageAt is when the customer last wrote, or nil.
	LastReceivedMessageAt *time.Time `json:"lastReceivedMessageAt,omitempty"`
	// LastMessageAt is when the last message was sent, or nil.
	LastMessageAt *time.Time `json:"lastMessageAt,omitempty"`
	// ClosedAt is when the ticket was closed, or nil.
	ClosedAt *time.Time `json:"closedAt,omitempty"`
	// SnoozedUntil is when Gorgias reopens a snoozed ticket, or nil.
	SnoozedUntil *time.Time `json:"snoozedUntil,omitempty"`
	// TrashedAt is when the ticket was moved to the trash, or nil.
	TrashedAt *time.Time `json:"trashedAt,omitempty"`
}

// Customer is the connector's view of one Gorgias customer, the requester of a ticket.
type Customer struct {
	// ID is the Gorgias customer ID, which searchTickets accepts as RequesterID.
	ID int64 `json:"id"`
	// Email is the customer's primary email address, or empty.
	Email string `json:"email,omitempty"`
	// Name is the customer's full name, or empty.
	Name string `json:"name,omitempty"`
	// ExternalID is the customer's ID in another system, such as a CRM, or empty.
	ExternalID string `json:"externalId,omitempty"`
	// Language is the customer's preferred ISO 639-1 language, or empty.
	Language string `json:"language,omitempty"`
}

// TicketMessage is one message on a ticket: a customer message, an agent reply, or an internal note.
type TicketMessage struct {
	// ID is the Gorgias message ID.
	ID int64 `json:"id"`
	// TicketID is the ticket the message belongs to.
	TicketID int64 `json:"ticketId"`
	// Channel is the message channel, such as MessageChannelEmail or MessageChannelInternalNote.
	Channel string `json:"channel"`
	// Via is how the message was received or sent, such as email or api.
	Via string `json:"via,omitempty"`
	// IsPublic is false for an internal note, which only agents see.
	IsPublic bool `json:"isPublic"`
	// IsFromAgent is true for a message your company sent, false for a customer's message.
	IsFromAgent bool `json:"isFromAgent"`
	// SenderID is the user or customer who sent the message, or zero.
	SenderID int64 `json:"senderId,omitempty"`
	// Subject is the message subject, or empty.
	Subject string `json:"subject,omitempty"`
	// Body is the message as plain text: Gorgias's body_text, or its stripped_text when that is empty.
	Body string `json:"body"`
	// IsBodyTruncated reports that Body was cut at MaxTextBytes.
	IsBodyTruncated bool `json:"isBodyTruncated,omitempty"`
	// ExternalID is the message's external ID. Messages addNote adds carry its idempotency key.
	ExternalID string `json:"externalId,omitempty"`
	// CreatedAt is when the message was created.
	CreatedAt time.Time `json:"createdAt"`
	// SentAt is when Gorgias sent the message, or nil. Gorgias sends replies asynchronously, so a
	// reply addNote just added has none yet.
	SentAt *time.Time `json:"sentAt,omitempty"`
	// FailedAt is when Gorgias failed to send the message, or nil.
	FailedAt *time.Time `json:"failedAt,omitempty"`
}

// MaxTextBytes bounds each message body a Result carries, so durable Step state stays small.
// Longer text is cut at a UTF-8 boundary and flagged.
const MaxTextBytes = 16 << 10

const (
	// MaxSubjectLength is Gorgias's limit for a ticket subject, in characters.
	MaxSubjectLength = 998
	// MaxTagLength is Gorgias's limit for a tag name, in characters.
	MaxTagLength = 256
	// MaxTagsPerChange bounds the tags one input adds, removes, or filters by.
	MaxTagsPerChange = 20
)

// validateTicketStatus accepts only Gorgias's two statuses, so a sibling desk's value is rejected locally.
func validateTicketStatus(fieldName string, status TicketStatus) error {
	if !slices.Contains(TicketStatuses(), status) {
		return fmt.Errorf("%s %q is not a Gorgias status; use open or closed", fieldName, status)
	}
	return nil
}

func validateTicketPriority(fieldName string, priority TicketPriority) error {
	if !slices.Contains(TicketPriorities(), priority) {
		return fmt.Errorf("%s %q is not a Gorgias priority; use low, normal, high, or critical", fieldName, priority)
	}
	return nil
}

// validateTags requires distinct, trimmed names without control characters; Gorgias compares names case sensitively.
func validateTags(fieldName string, tags []string) error {
	if len(tags) > MaxTagsPerChange {
		return fmt.Errorf("%s lists more than %d tags", fieldName, MaxTagsPerChange)
	}
	for index, tag := range tags {
		if tag == "" || tag != strings.TrimSpace(tag) || !utf8.ValidString(tag) || utf8.RuneCountInString(tag) > MaxTagLength ||
			strings.ContainsFunc(tag, unicode.IsControl) {
			return fmt.Errorf("%s entry %d must be 1 to %d characters without surrounding spaces or control characters", fieldName, index, MaxTagLength)
		}
		if slices.Contains(tags[:index], tag) {
			return fmt.Errorf("%s lists %q twice", fieldName, tag)
		}
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

func isBareEmailAddress(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Name == "" && address.Address == value && !strings.ContainsAny(value, " \"'()<>,;")
}

// plainTextToGorgiasHTML escapes text for body_html, keeping line breaks as <br>.
func plainTextToGorgiasHTML(text string) string {
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

// gorgiasTimestampLayouts accept an explicit offset and Gorgias's documented offset-less UTC form.
var gorgiasTimestampLayouts = []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05"}

// parseGorgiasTimestamp reads Gorgias's ISO 8601 timestamps; one without an offset is UTC.
func parseGorgiasTimestamp(value string) (time.Time, error) {
	for _, layout := range gorgiasTimestampLayouts {
		if instant, err := time.Parse(layout, value); err == nil {
			return instant.UTC(), nil
		}
	}
	return time.Time{}, errors.New("timestamp is not ISO 8601")
}

func parseOptionalTimestamp(fieldName string, value *string) (*time.Time, error) {
	if value == nil || *value == "" {
		return nil, nil
	}
	instant, err := parseGorgiasTimestamp(*value)
	if err != nil {
		return nil, fmt.Errorf("%s is not an ISO 8601 timestamp", fieldName)
	}
	return &instant, nil
}
