// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaxThreadBodyBytes bounds each thread body a Result carries; a longer body is cut on a UTF-8 boundary.
	MaxThreadBodyBytes = 16 << 10
	// maxPreviewBytes bounds the conversation preview, which Help Scout derives from the newest thread.
	maxPreviewBytes = 1 << 10
	// maxTagBytes bounds one tag name; Help Scout documents no limit.
	maxTagBytes = 100
)

// ConversationStatus is a Help Scout conversation status. The connector takes and returns Help Scout's own
// values and never maps them to another vocabulary; a Result passes through any value Help Scout adds.
type ConversationStatus string

const (
	// ConversationStatusActive is an open conversation that waits for the team.
	ConversationStatusActive ConversationStatus = "active"
	// ConversationStatusPending is an open conversation that waits for the customer or a later action.
	ConversationStatusPending ConversationStatus = "pending"
	// ConversationStatusClosed is a resolved conversation; a customer reply reopens it.
	ConversationStatusClosed ConversationStatus = "closed"
	// ConversationStatusSpam is a conversation marked as spam.
	ConversationStatusSpam ConversationStatus = "spam"
	// ConversationStatusAll is a searchConversations filter value only: conversations in every status.
	ConversationStatusAll ConversationStatus = "all"
)

// ConversationStatuses returns Help Scout's conversation statuses in the order its documentation lists
// them for an open-to-closed lifecycle: active, pending, closed, and spam.
func ConversationStatuses() []ConversationStatus {
	return []ConversationStatus{ConversationStatusActive, ConversationStatusPending, ConversationStatusClosed, ConversationStatusSpam}
}

// Person is a Help Scout user, team, or customer as a conversation or thread names it.
type Person struct {
	// ID is the Help Scout user, team, or customer ID.
	ID int64 `json:"id"`
	// Type is Help Scout's person type, such as user, team, customer, or system_user for an AI agent
	// in V3 webhook payloads.
	Type string `json:"type,omitempty"`
	// Email is the person's email address when Help Scout returns one.
	Email string `json:"email,omitempty"`
	// FirstName is the person's first name, or a team name.
	FirstName string `json:"firstName,omitempty"`
	// LastName is the person's last name.
	LastName string `json:"lastName,omitempty"`
}

// Conversation is the connector-safe subset of one Help Scout conversation. Times are UTC.
type Conversation struct {
	// ID is the stable conversation ID used by every operation.
	ID int64 `json:"id"`
	// Number is the conversation number shown in the Help Scout interface.
	Number int64 `json:"number"`
	// Subject is the conversation subject.
	Subject string `json:"subject"`
	// Status is Help Scout's status: active, pending, closed, or spam.
	Status ConversationStatus `json:"status"`
	// State is published, draft, or deleted.
	State string `json:"state,omitempty"`
	// Type is email, chat, or phone.
	Type string `json:"type,omitempty"`
	// MailboxID is the inbox that holds the conversation.
	MailboxID int64 `json:"mailboxId"`
	// FolderID is the inbox folder that holds the conversation.
	FolderID int64 `json:"folderId,omitempty"`
	// Preview is Help Scout's preview of the newest thread, cut at 1 KiB.
	Preview string `json:"preview,omitempty"`
	// ThreadCount is Help Scout's count of published threads, excluding notes.
	ThreadCount int `json:"threadCount"`
	// Assignee is the user or team that owns the conversation, or nil when it is unassigned.
	Assignee *Person `json:"assignee,omitempty"`
	// PrimaryCustomer is the customer a reply is sent to, or nil when Help Scout names none.
	PrimaryCustomer *Person `json:"primaryCustomer,omitempty"`
	// CreatedBy is the customer or user who started the conversation.
	CreatedBy *Person `json:"createdBy,omitempty"`
	// Tags lists the conversation's tag names.
	Tags []string `json:"tags"`
	// SourceType is how the conversation arrived, such as email, chat, api, or beacon.
	SourceType string `json:"sourceType,omitempty"`
	// SourceVia is customer or user.
	SourceVia string `json:"sourceVia,omitempty"`
	// CreatedAt is when the conversation was created, or zero when Help Scout omitted it.
	CreatedAt time.Time `json:"createdAt,omitzero"`
	// ClosedAt is when the conversation was closed, or zero.
	ClosedAt time.Time `json:"closedAt,omitzero"`
	// UserUpdatedAt is when a user last changed the conversation, or zero.
	UserUpdatedAt time.Time `json:"userUpdatedAt,omitzero"`
	// CustomerWaitingSince is when the customer started waiting for an answer, or zero.
	CustomerWaitingSince time.Time `json:"customerWaitingSince,omitzero"`
	// WebURL opens the conversation in the Help Scout interface.
	WebURL string `json:"webUrl,omitempty"`
}

// Thread is the connector-safe subset of one conversation thread. Times are UTC.
type Thread struct {
	// ID is the thread ID.
	ID int64 `json:"id"`
	// Type is customer for a customer's message, message for a user's reply, note for an internal note,
	// lineitem for a change of state, or chat, phone, beaconchat, forwardparent, or forwardchild.
	Type string `json:"type"`
	// Status is the conversation status this thread set.
	Status ConversationStatus `json:"status,omitempty"`
	// State is published, draft, hidden, bounced, or review.
	State string `json:"state,omitempty"`
	// Body is the thread's HTML body, cut at MaxThreadBodyBytes; a lineitem has none.
	Body string `json:"body,omitempty"`
	// IsBodyTruncated reports that Body was cut at MaxThreadBodyBytes.
	IsBodyTruncated bool `json:"isBodyTruncated,omitempty"`
	// CreatedBy is the user or customer who wrote the thread.
	CreatedBy *Person `json:"createdBy,omitempty"`
	// SourceType is how the thread arrived, such as email, api, or web.
	SourceType string `json:"sourceType,omitempty"`
	// SourceVia is customer or user.
	SourceVia string `json:"sourceVia,omitempty"`
	// CreatedAt is when the thread was created.
	CreatedAt time.Time `json:"createdAt"`
}

type helpScoutPersonWire struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Email string `json:"email"`
	First string `json:"first"`
	Last  string `json:"last"`
}

type helpScoutSourceWire struct {
	Type string `json:"type"`
	Via  string `json:"via"`
}

type helpScoutConversationWire struct {
	ID                   int64                `json:"id"`
	Number               int64                `json:"number"`
	Threads              int                  `json:"threads"`
	Type                 string               `json:"type"`
	FolderID             int64                `json:"folderId"`
	Status               string               `json:"status"`
	State                string               `json:"state"`
	Subject              string               `json:"subject"`
	Preview              string               `json:"preview"`
	MailboxID            int64                `json:"mailboxId"`
	Assignee             *helpScoutPersonWire `json:"assignee"`
	CreatedBy            *helpScoutPersonWire `json:"createdBy"`
	PrimaryCustomer      *helpScoutPersonWire `json:"primaryCustomer"`
	CreatedAt            string               `json:"createdAt"`
	ClosedAt             string               `json:"closedAt"`
	UserUpdatedAt        string               `json:"userUpdatedAt"`
	CustomerWaitingSince *struct {
		Time string `json:"time"`
	} `json:"customerWaitingSince"`
	Source *helpScoutSourceWire `json:"source"`
	Tags   []struct {
		Tag string `json:"tag"`
	} `json:"tags"`
	Links struct {
		Web struct {
			Href string `json:"href"`
		} `json:"web"`
	} `json:"_links"`
}

type helpScoutThreadWire struct {
	ID        int64                `json:"id"`
	Type      string               `json:"type"`
	Status    string               `json:"status"`
	State     string               `json:"state"`
	Body      string               `json:"body"`
	Source    *helpScoutSourceWire `json:"source"`
	CreatedBy *helpScoutPersonWire `json:"createdBy"`
	CreatedAt string               `json:"createdAt"`
}

// helpScoutPageLinks are the HAL links of a Help Scout collection; next is absent on the last page.
type helpScoutPageLinks struct {
	Next *struct {
		Href string `json:"href"`
	} `json:"next"`
}

// decodeConversationBody decodes a GET /v2/conversations/{id} body that must describe conversationID.
func decodeConversationBody(body []byte, conversationID int64) (Conversation, error) {
	var wire helpScoutConversationWire
	if err := decodeHelpScoutJSON(body, &wire); err != nil {
		return Conversation{}, errors.New("conversation is not a JSON object")
	}
	conversation, err := decodeConversation(wire)
	if err != nil {
		return Conversation{}, err
	}
	if conversation.ID != conversationID {
		return Conversation{}, errors.New("conversation response is for another conversation")
	}
	return conversation, nil
}

// decodeConversation requires only an ID, so a webhook body missing a field is not refused on every retry.
func decodeConversation(wire helpScoutConversationWire) (Conversation, error) {
	if wire.ID < 1 {
		return Conversation{}, errors.New("conversation has no ID")
	}
	createdAt, err := parseOptionalHelpScoutTimestamp(wire.CreatedAt)
	if err != nil {
		return Conversation{}, fmt.Errorf("conversation %d createdAt: %w", wire.ID, err)
	}
	closedAt, err := parseOptionalHelpScoutTimestamp(wire.ClosedAt)
	if err != nil {
		return Conversation{}, fmt.Errorf("conversation %d closedAt: %w", wire.ID, err)
	}
	userUpdatedAt, err := parseOptionalHelpScoutTimestamp(wire.UserUpdatedAt)
	if err != nil {
		return Conversation{}, fmt.Errorf("conversation %d userUpdatedAt: %w", wire.ID, err)
	}
	var customerWaitingSince time.Time
	if wire.CustomerWaitingSince != nil {
		if customerWaitingSince, err = parseOptionalHelpScoutTimestamp(wire.CustomerWaitingSince.Time); err != nil {
			return Conversation{}, fmt.Errorf("conversation %d customerWaitingSince: %w", wire.ID, err)
		}
	}
	conversation := Conversation{
		ID: wire.ID, Number: wire.Number, Subject: wire.Subject, Status: ConversationStatus(wire.Status), State: wire.State,
		Type: wire.Type, MailboxID: wire.MailboxID, FolderID: wire.FolderID, Preview: truncateUTF8(wire.Preview, maxPreviewBytes),
		ThreadCount: wire.Threads, Assignee: decodePerson(wire.Assignee), PrimaryCustomer: decodePerson(wire.PrimaryCustomer),
		CreatedBy: decodePerson(wire.CreatedBy), Tags: make([]string, 0, len(wire.Tags)),
		CreatedAt: createdAt, ClosedAt: closedAt, UserUpdatedAt: userUpdatedAt, CustomerWaitingSince: customerWaitingSince,
		WebURL: httpsURLOrEmpty(wire.Links.Web.Href),
	}
	if wire.Source != nil {
		conversation.SourceType, conversation.SourceVia = wire.Source.Type, wire.Source.Via
	}
	for _, tag := range wire.Tags {
		if tag.Tag != "" {
			conversation.Tags = append(conversation.Tags, tag.Tag)
		}
	}
	return conversation, nil
}

func decodeThread(wire helpScoutThreadWire) (Thread, error) {
	if wire.ID < 1 || wire.Type == "" {
		return Thread{}, errors.New("thread has no ID or type")
	}
	createdAt, err := parseHelpScoutTimestamp(wire.CreatedAt)
	if err != nil {
		return Thread{}, fmt.Errorf("thread %d createdAt: %w", wire.ID, err)
	}
	body := truncateUTF8(wire.Body, MaxThreadBodyBytes)
	thread := Thread{
		ID: wire.ID, Type: wire.Type, Status: ConversationStatus(wire.Status), State: wire.State,
		Body: body, IsBodyTruncated: len(body) < len(wire.Body), CreatedBy: decodePerson(wire.CreatedBy), CreatedAt: createdAt,
	}
	if wire.Source != nil {
		thread.SourceType, thread.SourceVia = wire.Source.Type, wire.Source.Via
	}
	return thread, nil
}

// decodePerson returns nil for an absent person or one without an ID, such as an unassigned conversation.
func decodePerson(wire *helpScoutPersonWire) *Person {
	if wire == nil || wire.ID < 1 {
		return nil
	}
	return &Person{ID: wire.ID, Type: wire.Type, Email: wire.Email, FirstName: wire.First, LastName: wire.Last}
}

// decodeHelpScoutJSON decodes a 2xx body; unknown fields are ignored because Help Scout adds fields over time.
func decodeHelpScoutJSON(body []byte, destination any) error {
	if len(body) == 0 || json.Unmarshal(body, destination) != nil {
		return errHelpScoutResponseMalformed
	}
	return nil
}

// parseHelpScoutTimestamp reads a required ISO 8601 UTC time, such as 2017-01-02T23:00:00Z.
func parseHelpScoutTimestamp(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, errors.New("timestamp is not ISO 8601")
	}
	return parsed.UTC(), nil
}

// parseOptionalHelpScoutTimestamp reads a time Help Scout may omit.
func parseOptionalHelpScoutTimestamp(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return parseHelpScoutTimestamp(value)
}

// formatHelpScoutTimestamp writes the yyyy-MM-dd'T'HH:mm:ss'Z' form Help Scout documents for parameters.
func formatHelpScoutTimestamp(value time.Time) string {
	return value.UTC().Truncate(time.Second).Format("2006-01-02T15:04:05Z")
}

// truncateUTF8 cuts value to at most limit bytes without splitting a UTF-8 sequence.
func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}

// httpsURLOrEmpty keeps an absolute HTTPS link without user information.
func httpsURLOrEmpty(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	return value
}

// validateConversationStatus accepts active, pending, closed, and spam.
func validateConversationStatus(field string, status ConversationStatus) error {
	for _, known := range ConversationStatuses() {
		if status == known {
			return nil
		}
	}
	return fmt.Errorf("%s must be one of Help Scout's statuses active, pending, closed, or spam", field)
}

// validateTags requires distinct, non-blank tag names without commas, which Help Scout uses as a separator.
func validateTags(field string, tags []string) error {
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		folded := strings.ToLower(tag)
		switch {
		case strings.TrimSpace(tag) != tag || tag == "":
			return fmt.Errorf("%s holds a blank tag or one with surrounding whitespace", field)
		case len(tag) > maxTagBytes:
			return fmt.Errorf("%s holds a tag longer than %d bytes", field, maxTagBytes)
		case strings.ContainsAny(tag, ",\"()\\") || strings.IndexFunc(tag, isControlRune) >= 0:
			return fmt.Errorf("%s tag %q holds a comma, quote, parenthesis, backslash, or control character", field, tag)
		case seen[folded]:
			return fmt.Errorf("%s lists tag %q twice", field, tag)
		}
		seen[folded] = true
	}
	return nil
}

func isControlRune(character rune) bool {
	return character < 0x20 || character == 0x7f
}
