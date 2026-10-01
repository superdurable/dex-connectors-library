// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ConversationState is Intercom's own conversation state. The connector takes and returns these
// values unchanged and never maps them to another vocabulary. A Result passes through any value
// Intercom adds later.
type ConversationState string

const (
	// ConversationStateOpen is a conversation waiting in a teammate's or team's inbox.
	ConversationStateOpen ConversationState = "open"
	// ConversationStateClosed is a conversation a teammate closed; a customer reply reopens it.
	ConversationStateClosed ConversationState = "closed"
	// ConversationStateSnoozed is a conversation hidden until its SnoozedUntil time, when Intercom reopens it.
	ConversationStateSnoozed ConversationState = "snoozed"
)

// ConversationStates returns Intercom's three conversation states in Intercom's order, so a Flow can
// map them to its own vocabulary explicitly.
func ConversationStates() []ConversationState {
	return []ConversationState{ConversationStateOpen, ConversationStateClosed, ConversationStateSnoozed}
}

// MaxTextBytes bounds each message body a Result carries, so durable Step state stays small. Longer
// text is cut at a UTF-8 boundary and flagged as truncated.
const MaxTextBytes = 16 << 10

// Conversation is the connector's view of one Intercom conversation without its parts. Intercom's
// statistics, custom attributes, SLA, rating, company, AI agent, and linked objects are omitted. Empty
// assignee IDs mean the conversation is unassigned.
type Conversation struct {
	// ID is the Intercom conversation ID, a string of digits such as 215472658213.
	ID string `json:"id"`
	// Title is the conversation title, or empty when it has none.
	Title string `json:"title,omitempty"`
	// State is Intercom's state: open, closed, or snoozed.
	State ConversationState `json:"state"`
	// IsOpen is Intercom's open flag; it is true for an open or snoozed conversation.
	IsOpen bool `json:"isOpen"`
	// IsRead is Intercom's read flag for the conversation.
	IsRead bool `json:"isRead,omitempty"`
	// Priority is Intercom's priority value, such as none, low, medium, high, or urgent in API version 2.16.
	Priority string `json:"priority,omitempty"`
	// AdminAssigneeID is the assigned admin, or empty when no admin is assigned.
	AdminAssigneeID string `json:"adminAssigneeId,omitempty"`
	// TeamAssigneeID is the assigned team, or empty when no team is assigned.
	TeamAssigneeID string `json:"teamAssigneeId,omitempty"`
	// Source is the message that started the conversation. Its Body is set only by getConversation.
	Source ConversationSource `json:"source"`
	// Contacts lists the users and leads in the conversation; it holds one unless more were added.
	Contacts []ConversationContact `json:"contacts,omitempty"`
	// Tags lists the conversation's tags.
	Tags []ConversationTag `json:"tags,omitempty"`
	// CreatedAt is when the conversation was created.
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is when the conversation last changed.
	UpdatedAt time.Time `json:"updatedAt"`
	// WaitingSince is when the customer started waiting for a reply, or nil when an admin replied last.
	WaitingSince *time.Time `json:"waitingSince,omitempty"`
	// SnoozedUntil is when a snoozed conversation reopens, or nil.
	SnoozedUntil *time.Time `json:"snoozedUntil,omitempty"`
}

// ConversationSource is the message that started a conversation.
type ConversationSource struct {
	// Type is how the conversation began, such as conversation, email, or whatsapp.
	Type string `json:"type,omitempty"`
	// DeliveredAs is Intercom's value for how the conversation was initiated, such as operator_initiated.
	DeliveredAs string `json:"deliveredAs,omitempty"`
	// Subject is the message subject as plain text, or empty.
	Subject string `json:"subject,omitempty"`
	// Body is the first message as plain text. Only getConversation sets it.
	Body string `json:"body,omitempty"`
	// IsBodyTruncated reports that Body was cut at MaxTextBytes.
	IsBodyTruncated bool `json:"isBodyTruncated,omitempty"`
	// Author is who sent the first message: a contact for an inbound conversation, or an admin or bot.
	Author ConversationAuthor `json:"author"`
}

// ConversationAuthor is the sender of a message or conversation part.
type ConversationAuthor struct {
	// Type is Intercom's author type, such as user, lead, admin, bot, or team.
	Type string `json:"type,omitempty"`
	// ID is the author's Intercom ID: a contact ID for a user or lead, an admin ID for an admin.
	ID string `json:"id,omitempty"`
	// Name is the author's display name, or empty.
	Name string `json:"name,omitempty"`
	// Email is the author's email address, or empty when Intercom has none.
	Email string `json:"email,omitempty"`
}

// ConversationContact references a user or lead in a conversation.
type ConversationContact struct {
	// ID is the Intercom contact ID.
	ID string `json:"id"`
	// ExternalID is the application's own ID for the contact, or empty.
	ExternalID string `json:"externalId,omitempty"`
}

// ConversationTag is a tag applied to a conversation.
type ConversationTag struct {
	// ID is the Intercom tag ID.
	ID string `json:"id"`
	// Name is the tag name.
	Name string `json:"name,omitempty"`
}

// ConversationPart is one message or event in a conversation, such as a reply, note, or close.
type ConversationPart struct {
	// ID is the Intercom conversation part ID.
	ID string `json:"id"`
	// PartType is Intercom's part type, such as comment, note, close, open, snoozed, or assignment.
	PartType string `json:"partType"`
	// Body is the part's message as plain text, or empty for an event without one.
	Body string `json:"body,omitempty"`
	// IsBodyTruncated reports that Body was cut at MaxTextBytes.
	IsBodyTruncated bool `json:"isBodyTruncated,omitempty"`
	// Author is who created the part.
	Author ConversationAuthor `json:"author"`
	// CreatedAt is when the part was created.
	CreatedAt time.Time `json:"createdAt"`
}

var (
	conversationIDPattern = regexp.MustCompile(`^[0-9]{1,24}$`)
	adminIDPattern        = regexp.MustCompile(`^[0-9]{1,24}$`)
	tagIDPattern          = regexp.MustCompile(`^[0-9]{1,24}$`)
	contactIDPattern      = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	integerTextPattern    = regexp.MustCompile(`^-?[0-9]{1,24}$`)
)

// intercomConversationWire is the conversation JSON the connector reads; Intercom's nulls decode as zero values.
type intercomConversationWire struct {
	Type            string             `json:"type"`
	ID              flexibleID         `json:"id"`
	Title           string             `json:"title"`
	State           string             `json:"state"`
	Open            bool               `json:"open"`
	Read            bool               `json:"read"`
	Priority        string             `json:"priority"`
	AdminAssigneeID flexibleID         `json:"admin_assignee_id"`
	TeamAssigneeID  flexibleID         `json:"team_assignee_id"`
	CreatedAt       unixSeconds        `json:"created_at"`
	UpdatedAt       unixSeconds        `json:"updated_at"`
	WaitingSince    unixSeconds        `json:"waiting_since"`
	SnoozedUntil    unixSeconds        `json:"snoozed_until"`
	Source          intercomSourceWire `json:"source"`
	Contacts        struct {
		Contacts []struct {
			ID         flexibleID `json:"id"`
			ExternalID string     `json:"external_id"`
		} `json:"contacts"`
	} `json:"contacts"`
	Tags struct {
		Tags []struct {
			ID   flexibleID `json:"id"`
			Name string     `json:"name"`
		} `json:"tags"`
	} `json:"tags"`
	ConversationParts struct {
		Parts      []intercomPartWire `json:"conversation_parts"`
		TotalCount int                `json:"total_count"`
	} `json:"conversation_parts"`
}

type intercomSourceWire struct {
	Type        string             `json:"type"`
	DeliveredAs string             `json:"delivered_as"`
	Subject     string             `json:"subject"`
	Body        string             `json:"body"`
	Author      intercomAuthorWire `json:"author"`
}

type intercomAuthorWire struct {
	Type  string     `json:"type"`
	ID    flexibleID `json:"id"`
	Name  string     `json:"name"`
	Email string     `json:"email"`
}

type intercomPartWire struct {
	ID        flexibleID         `json:"id"`
	PartType  string             `json:"part_type"`
	Body      string             `json:"body"`
	Author    intercomAuthorWire `json:"author"`
	CreatedAt unixSeconds        `json:"created_at"`
}

// flexibleID accepts a string, integer, or null ID, because ID types differ across API versions.
type flexibleID string

// UnmarshalJSON accepts a string, an integer, or null.
func (id *flexibleID) UnmarshalJSON(contents []byte) error {
	trimmed := bytes.TrimSpace(contents)
	if bytes.Equal(trimmed, []byte("null")) {
		*id = ""
		return nil
	}
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return errors.New("identifier is not a string")
		}
		*id = flexibleID(text)
		return nil
	}
	if !integerTextPattern.Match(trimmed) {
		return errors.New("identifier is not a string or an integer")
	}
	*id = flexibleID(trimmed)
	return nil
}

// unixSeconds decodes an Intercom timestamp sent as integer seconds, a numeric string, or null.
type unixSeconds int64

// UnmarshalJSON accepts an integer, a numeric string, or null.
func (seconds *unixSeconds) UnmarshalJSON(contents []byte) error {
	var id flexibleID
	if err := id.UnmarshalJSON(contents); err != nil {
		return errors.New("timestamp is not Unix seconds")
	}
	if id == "" {
		*seconds = 0
		return nil
	}
	value, err := strconv.ParseInt(string(id), 10, 64)
	if err != nil || value < 0 {
		return errors.New("timestamp is not Unix seconds")
	}
	*seconds = unixSeconds(value)
	return nil
}

// time returns the instant in UTC, or the zero time for a missing timestamp.
func (seconds unixSeconds) time() time.Time {
	if seconds <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(seconds), 0).UTC()
}

func (seconds unixSeconds) optionalTime() *time.Time {
	if seconds <= 0 {
		return nil
	}
	instant := seconds.time()
	return &instant
}

// decodeConversationWire validates one conversation; includeSourceBody keeps the first message's text.
func decodeConversationWire(wire intercomConversationWire, includeSourceBody bool) (Conversation, error) {
	if wire.Type != "" && wire.Type != "conversation" {
		return Conversation{}, errors.New("object is not a conversation")
	}
	if !conversationIDPattern.MatchString(string(wire.ID)) {
		return Conversation{}, errors.New("conversation ID is missing or not numeric")
	}
	conversation := Conversation{
		ID: string(wire.ID), Title: wire.Title, State: ConversationState(wire.State), IsOpen: wire.Open, IsRead: wire.Read,
		Priority: wire.Priority, AdminAssigneeID: assigneeID(wire.AdminAssigneeID), TeamAssigneeID: assigneeID(wire.TeamAssigneeID),
		Source: ConversationSource{
			Type: wire.Source.Type, DeliveredAs: wire.Source.DeliveredAs, Subject: wire.Source.Subject,
			Author: decodeAuthorWire(wire.Source.Author),
		},
		CreatedAt: wire.CreatedAt.time(), UpdatedAt: wire.UpdatedAt.time(),
		WaitingSince: wire.WaitingSince.optionalTime(), SnoozedUntil: wire.SnoozedUntil.optionalTime(),
	}
	if includeSourceBody {
		conversation.Source.Body, conversation.Source.IsBodyTruncated = truncateUTF8(wire.Source.Body, MaxTextBytes)
	}
	for _, contact := range wire.Contacts.Contacts {
		if contact.ID == "" {
			return Conversation{}, errors.New("conversation contact ID is missing")
		}
		conversation.Contacts = append(conversation.Contacts, ConversationContact{ID: string(contact.ID), ExternalID: contact.ExternalID})
	}
	for _, tag := range wire.Tags.Tags {
		if tag.ID == "" {
			return Conversation{}, errors.New("conversation tag ID is missing")
		}
		conversation.Tags = append(conversation.Tags, ConversationTag{ID: string(tag.ID), Name: tag.Name})
	}
	return conversation, nil
}

// decodeConversationEnvelope decodes a conversation response body, which Intercom sends unwrapped.
func decodeConversationEnvelope(body []byte, conversationID string) (intercomConversationWire, error) {
	var wire intercomConversationWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return intercomConversationWire{}, errors.New("conversation response is not a conversation object")
	}
	if string(wire.ID) != conversationID {
		return intercomConversationWire{}, errors.New("conversation response is for another conversation")
	}
	return wire, nil
}

func decodeAuthorWire(wire intercomAuthorWire) ConversationAuthor {
	return ConversationAuthor{Type: wire.Type, ID: string(wire.ID), Name: wire.Name, Email: wire.Email}
}

func decodePartWire(wire intercomPartWire) (ConversationPart, error) {
	if wire.ID == "" {
		return ConversationPart{}, errors.New("conversation part ID is missing")
	}
	body, isBodyTruncated := truncateUTF8(wire.Body, MaxTextBytes)
	return ConversationPart{
		ID: string(wire.ID), PartType: wire.PartType, Body: body, IsBodyTruncated: isBodyTruncated,
		Author: decodeAuthorWire(wire.Author), CreatedAt: wire.CreatedAt.time(),
	}, nil
}

// assigneeID maps Intercom's unassigned values, 0 in API version 2.16 and null before it, to empty.
func assigneeID(id flexibleID) string {
	if id == "0" {
		return ""
	}
	return string(id)
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

func validateConversationState(state ConversationState) error {
	for _, known := range ConversationStates() {
		if state == known {
			return nil
		}
	}
	return fmt.Errorf("state %q is not an Intercom conversation state; use open, closed, or snoozed", state)
}

func validateConversationID(fieldName string, conversationID string) error {
	if !conversationIDPattern.MatchString(conversationID) {
		return fmt.Errorf("%s must be an Intercom conversation ID of digits, such as 215472658213", fieldName)
	}
	return nil
}

func validateAdminID(adminID string) error {
	if !adminIDPattern.MatchString(adminID) {
		return errors.New("adminId must be an Intercom admin ID of digits, such as 5017691; choose one with the adminPicker unit")
	}
	return nil
}

func isBareEmailAddress(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Name == "" && address.Address == value && !strings.ContainsAny(value, " \"'()<>,;")
}
