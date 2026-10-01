// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// TicketStatus is Zendesk's own ticket status value. With custom ticket statuses
// enabled, Zendesk reports the status category here and the custom status in
// Ticket.CustomStatusID. Results pass through any value Zendesk adds later.
type TicketStatus string

const (
	// TicketStatusNew is a ticket no agent has opened or changed yet.
	TicketStatusNew TicketStatus = "new"
	// TicketStatusOpen is a ticket waiting for an agent.
	TicketStatusOpen TicketStatus = "open"
	// TicketStatusPending is a ticket waiting for the requester.
	TicketStatusPending TicketStatus = "pending"
	// TicketStatusHold is a ticket waiting for a third party.
	TicketStatusHold TicketStatus = "hold"
	// TicketStatusSolved is a ticket an agent marked solved; the requester can still reopen it.
	TicketStatusSolved TicketStatus = "solved"
	// TicketStatusClosed is a solved ticket Zendesk locked; it can no longer be updated.
	TicketStatusClosed TicketStatus = "closed"
)

// TicketPriority is Zendesk's own ticket priority value.
type TicketPriority string

const (
	// TicketPriorityLow is Zendesk's lowest priority.
	TicketPriorityLow TicketPriority = "low"
	// TicketPriorityNormal is Zendesk's default priority.
	TicketPriorityNormal TicketPriority = "normal"
	// TicketPriorityHigh is Zendesk's high priority.
	TicketPriorityHigh TicketPriority = "high"
	// TicketPriorityUrgent is Zendesk's highest priority.
	TicketPriorityUrgent TicketPriority = "urgent"
)

// TicketType is Zendesk's own ticket type value.
type TicketType string

const (
	// TicketTypeProblem is a ticket other incident tickets can link to.
	TicketTypeProblem TicketType = "problem"
	// TicketTypeIncident is one occurrence of a problem.
	TicketTypeIncident TicketType = "incident"
	// TicketTypeQuestion is a request for information.
	TicketTypeQuestion TicketType = "question"
	// TicketTypeTask is a ticket with a due date.
	TicketTypeTask TicketType = "task"
)

// TicketStatuses returns Zendesk's six status values in Zendesk's order, so a
// Flow can map them to its own vocabulary explicitly.
func TicketStatuses() []TicketStatus {
	return []TicketStatus{TicketStatusNew, TicketStatusOpen, TicketStatusPending, TicketStatusHold, TicketStatusSolved, TicketStatusClosed}
}

// Ticket is the connector's view of one Zendesk ticket. Zendesk's API URL,
// satisfaction rating, custom fields, collaborators, and via source details are
// omitted. Zero IDs mean Zendesk reported no value, such as an unassigned ticket.
type Ticket struct {
	// ID is the Zendesk ticket ID.
	ID int64 `json:"id"`
	// AgentURL opens the ticket in Agent Workspace, such as https://acme.zendesk.com/agent/tickets/35436.
	AgentURL string `json:"agentUrl"`
	// Subject is the ticket subject; Zendesk allows tickets created by API or email without one.
	Subject string `json:"subject,omitempty"`
	// Description is the first comment's text. searchTickets leaves it empty to keep pages small.
	Description string `json:"description,omitempty"`
	// IsDescriptionTruncated reports that Description was cut at MaxTextBytes.
	IsDescriptionTruncated bool `json:"isDescriptionTruncated,omitempty"`
	// Status is Zendesk's status value.
	Status TicketStatus `json:"status"`
	// CustomStatusID is the custom ticket status when the account enables custom statuses.
	CustomStatusID int64 `json:"customStatusId,omitempty"`
	// Priority is Zendesk's priority value, or empty when the ticket has none.
	Priority TicketPriority `json:"priority,omitempty"`
	// Type is Zendesk's ticket type, or empty when the ticket has none.
	Type TicketType `json:"type,omitempty"`
	// RequesterID is the user who asked for support.
	RequesterID int64 `json:"requesterId,omitempty"`
	// SubmitterID is the user who created the ticket and authored its first comment.
	SubmitterID int64 `json:"submitterId,omitempty"`
	// AssigneeID is the assigned agent, or zero when the ticket is unassigned.
	AssigneeID int64 `json:"assigneeId,omitempty"`
	// GroupID is the assigned group, or zero.
	GroupID int64 `json:"groupId,omitempty"`
	// OrganizationID is the requester's organization for this ticket, or zero.
	OrganizationID int64 `json:"organizationId,omitempty"`
	// BrandID is the brand the ticket belongs to, or zero.
	BrandID int64 `json:"brandId,omitempty"`
	// Tags lists the ticket's tags in Zendesk's order.
	Tags []string `json:"tags,omitempty"`
	// ExternalID is the application correlation ID stored on the ticket; Zendesk does not require it to be unique.
	ExternalID string `json:"externalId,omitempty"`
	// Channel is how the ticket was created, such as api, email, or web.
	Channel string `json:"channel,omitempty"`
	// CreatedAt is when the ticket was created.
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is when a ticket event last changed the ticket, at Zendesk's one-second precision.
	UpdatedAt time.Time `json:"updatedAt"`
	// DueAt is a task ticket's due time.
	DueAt *time.Time `json:"dueAt,omitempty"`
}

// TicketUser is the connector's view of a Zendesk user.
type TicketUser struct {
	// ID is the Zendesk user ID.
	ID int64 `json:"id"`
	// Name is the user's display name.
	Name string `json:"name,omitempty"`
	// Email is the user's primary email address, or empty when Zendesk has none.
	Email string `json:"email,omitempty"`
	// Role is Zendesk's role value, such as end-user, agent, or admin.
	Role string `json:"role,omitempty"`
}

// TicketComment is one public reply or internal note.
type TicketComment struct {
	// ID is the Zendesk comment ID.
	ID int64 `json:"id"`
	// AuthorID is the user who wrote the comment.
	AuthorID int64 `json:"authorId,omitempty"`
	// IsPublic is true for a public reply the requester can see and false for an internal note.
	IsPublic bool `json:"isPublic"`
	// Body is the comment as plain text.
	Body string `json:"body"`
	// IsBodyTruncated reports that Body was cut at MaxTextBytes.
	IsBodyTruncated bool `json:"isBodyTruncated,omitempty"`
	// CreatedAt is when the comment was added.
	CreatedAt time.Time `json:"createdAt"`
}

// TicketCommentInput is one comment to add to a ticket.
type TicketCommentInput struct {
	// Body is the plain-text comment. It is required and is sent as Zendesk's body, so HTML is not rendered.
	Body string `json:"body"`
	// IsInternalNote adds the comment as an internal note that only agents can see; false adds a public reply
	// that Zendesk may email to the requester.
	IsInternalNote bool `json:"isInternalNote,omitempty"`
}

// MaxTextBytes bounds each ticket description and comment body a Result carries, so
// durable Step state stays small. Longer text is cut at a UTF-8 boundary and flagged.
const MaxTextBytes = 16 << 10

var tagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_./-]{0,79}$`)

// zendeskTicketWire is the ticket JSON the connector reads; Zendesk's nulls decode as zero values.
type zendeskTicketWire struct {
	ID             int64    `json:"id"`
	Subject        string   `json:"subject"`
	Description    string   `json:"description"`
	Status         string   `json:"status"`
	CustomStatusID int64    `json:"custom_status_id"`
	Priority       string   `json:"priority"`
	Type           string   `json:"type"`
	RequesterID    int64    `json:"requester_id"`
	SubmitterID    int64    `json:"submitter_id"`
	AssigneeID     int64    `json:"assignee_id"`
	GroupID        int64    `json:"group_id"`
	OrganizationID int64    `json:"organization_id"`
	BrandID        int64    `json:"brand_id"`
	Tags           []string `json:"tags"`
	ExternalID     string   `json:"external_id"`
	Via            struct {
		Channel json.RawMessage `json:"channel"`
	} `json:"via"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	DueAt     string `json:"due_at"`
}

type zendeskUserWire struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

type zendeskCommentWire struct {
	ID        int64  `json:"id"`
	AuthorID  int64  `json:"author_id"`
	Public    bool   `json:"public"`
	Body      string `json:"body"`
	PlainBody string `json:"plain_body"`
	CreatedAt string `json:"created_at"`
}

// decodedTicket keeps Zendesk's exact updated_at text, which a safe update sends back as updated_stamp.
type decodedTicket struct {
	ticket       Ticket
	updatedStamp string
}

// decodeTicketWire validates one ticket; agentTicketURL is the Agent Workspace ticket URL prefix.
func decodeTicketWire(wire zendeskTicketWire, agentTicketURL string) (decodedTicket, error) {
	if wire.ID < 1 {
		return decodedTicket{}, errors.New("ticket ID is missing")
	}
	if wire.Status == "" {
		return decodedTicket{}, errors.New("ticket status is missing")
	}
	createdAt, err := time.Parse(time.RFC3339, wire.CreatedAt)
	if err != nil {
		return decodedTicket{}, errors.New("ticket created_at is not RFC 3339")
	}
	updatedAt, err := time.Parse(time.RFC3339, wire.UpdatedAt)
	if err != nil {
		return decodedTicket{}, errors.New("ticket updated_at is not RFC 3339")
	}
	description, isDescriptionTruncated := truncateUTF8(wire.Description, MaxTextBytes)
	ticket := Ticket{
		ID: wire.ID, AgentURL: agentTicketURL + strconv.FormatInt(wire.ID, 10), Subject: wire.Subject,
		Description: description, IsDescriptionTruncated: isDescriptionTruncated,
		Status: TicketStatus(wire.Status), CustomStatusID: wire.CustomStatusID,
		Priority: TicketPriority(wire.Priority), Type: TicketType(wire.Type),
		RequesterID: wire.RequesterID, SubmitterID: wire.SubmitterID, AssigneeID: wire.AssigneeID,
		GroupID: wire.GroupID, OrganizationID: wire.OrganizationID, BrandID: wire.BrandID,
		Tags: wire.Tags, ExternalID: wire.ExternalID, Channel: viaChannel(wire.Via.Channel),
		CreatedAt: createdAt.UTC(), UpdatedAt: updatedAt.UTC(),
	}
	if wire.DueAt != "" {
		dueAt, err := time.Parse(time.RFC3339, wire.DueAt)
		if err != nil {
			return decodedTicket{}, errors.New("ticket due_at is not RFC 3339")
		}
		dueAt = dueAt.UTC()
		ticket.DueAt = &dueAt
	}
	return decodedTicket{ticket: ticket, updatedStamp: wire.UpdatedAt}, nil
}

// decodeTicketEnvelope decodes a {"ticket": {...}} response body.
func decodeTicketEnvelope(body []byte, agentTicketURL string) (decodedTicket, error) {
	var envelope struct {
		Ticket *zendeskTicketWire `json:"ticket"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return decodedTicket{}, errors.New("ticket response is not JSON")
	}
	if envelope.Ticket == nil {
		return decodedTicket{}, errors.New("ticket response has no ticket")
	}
	return decodeTicketWire(*envelope.Ticket, agentTicketURL)
}

func decodeUserWire(wire zendeskUserWire) TicketUser {
	return TicketUser{ID: wire.ID, Name: wire.Name, Email: wire.Email, Role: wire.Role}
}

func decodeCommentWire(wire zendeskCommentWire) (TicketComment, error) {
	if wire.ID < 1 {
		return TicketComment{}, errors.New("comment ID is missing")
	}
	createdAt, err := time.Parse(time.RFC3339, wire.CreatedAt)
	if err != nil {
		return TicketComment{}, errors.New("comment created_at is not RFC 3339")
	}
	text := wire.PlainBody
	if text == "" {
		text = wire.Body
	}
	body, isBodyTruncated := truncateUTF8(text, MaxTextBytes)
	return TicketComment{
		ID: wire.ID, AuthorID: wire.AuthorID, IsPublic: wire.Public, Body: body,
		IsBodyTruncated: isBodyTruncated, CreatedAt: createdAt.UTC(),
	}, nil
}

// viaChannel returns Zendesk's via.channel when it is a string; some channels report a number.
func viaChannel(raw json.RawMessage) string {
	var channel string
	if json.Unmarshal(raw, &channel) == nil {
		return channel
	}
	return strings.TrimSpace(string(raw))
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

func validateTicketStatus(status TicketStatus) error {
	for _, known := range TicketStatuses() {
		if status == known {
			return nil
		}
	}
	return fmt.Errorf("status %q is not a Zendesk status; use new, open, pending, hold, solved, or closed", status)
}

func validateTicketPriority(priority TicketPriority) error {
	switch priority {
	case TicketPriorityLow, TicketPriorityNormal, TicketPriorityHigh, TicketPriorityUrgent:
		return nil
	default:
		return fmt.Errorf("priority %q is not a Zendesk priority; use low, normal, high, or urgent", priority)
	}
}

func validateTicketType(ticketType TicketType) error {
	switch ticketType {
	case TicketTypeProblem, TicketTypeIncident, TicketTypeQuestion, TicketTypeTask:
		return nil
	default:
		return fmt.Errorf("type %q is not a Zendesk ticket type; use problem, incident, question, or task", ticketType)
	}
}

// validateTags requires lowercase ASCII tags; Zendesk stores tags lowercase, so uppercase never matches.
func validateTags(fieldName string, tags []string) error {
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if !tagPattern.MatchString(tag) {
			return fmt.Errorf("%s entry %q must be 1 to 80 lowercase letters, digits, and _ . / - characters, starting with a letter or digit", fieldName, tag)
		}
		if seen[tag] {
			return fmt.Errorf("%s lists %q twice", fieldName, tag)
		}
		seen[tag] = true
	}
	return nil
}

func validateCommentInput(fieldName string, comment TicketCommentInput) error {
	if strings.TrimSpace(comment.Body) == "" {
		return fmt.Errorf("%s.body is required", fieldName)
	}
	return nil
}

func validateOptionalID(fieldName string, id int64) error {
	if id < 0 {
		return fmt.Errorf("%s cannot be negative", fieldName)
	}
	return nil
}
