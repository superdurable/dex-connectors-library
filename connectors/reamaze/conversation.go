// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// ConversationStatus is Re:amaze's own integer conversation status, as GET /conversations
// documents it. Results pass through any value Re:amaze returns, including a future one.
type ConversationStatus int

const (
	// ConversationStatusOpen is Re:amaze's Open status (0): the conversation awaits a staff
	// response. A customer reply reopens a Done conversation to Open.
	ConversationStatusOpen ConversationStatus = 0
	// ConversationStatusResponded is Re:amaze's Responded status (1), which a staff reply sets
	// before the conversation is marked Done, On Hold, or Archived.
	ConversationStatusResponded ConversationStatus = 1
	// ConversationStatusDone is Re:amaze's Done status (2), a resolved conversation.
	ConversationStatusDone ConversationStatus = 2
	// ConversationStatusSpam is Re:amaze's Spam status (3), set by staff.
	ConversationStatusSpam ConversationStatus = 3
	// ConversationStatusArchived is Re:amaze's Archived status (4). Archiving marks an unresolved
	// conversation resolved, and searchConversations omits archived conversations by default.
	ConversationStatusArchived ConversationStatus = 4
	// ConversationStatusOnHold is Re:amaze's On Hold status (5): a reminder is set for HoldUntil.
	ConversationStatusOnHold ConversationStatus = 5
	// ConversationStatusAutoDone is Re:amaze's Auto-Done status (6), resolved by the Re:amaze Assistant.
	ConversationStatusAutoDone ConversationStatus = 6
	// ConversationStatusAIAgentAssigned is Re:amaze's AI Agent Assigned status (7).
	ConversationStatusAIAgentAssigned ConversationStatus = 7
	// ConversationStatusAIAgentDone is Re:amaze's AI Agent Done status (8), resolved by a chatbot.
	ConversationStatusAIAgentDone ConversationStatus = 8
	// ConversationStatusAISpam is Re:amaze's Spam (identified by AI) status (9).
	ConversationStatusAISpam ConversationStatus = 9
)

// ConversationStatuses returns every status Re:amaze documents, in Re:amaze's order, 0 to 9.
func ConversationStatuses() []ConversationStatus {
	return []ConversationStatus{
		ConversationStatusOpen, ConversationStatusResponded, ConversationStatusDone, ConversationStatusSpam,
		ConversationStatusArchived, ConversationStatusOnHold, ConversationStatusAutoDone,
		ConversationStatusAIAgentAssigned, ConversationStatusAIAgentDone, ConversationStatusAISpam,
	}
}

// MessageVisibility is Re:amaze's own integer message visibility.
type MessageVisibility int

const (
	// MessageVisibilityRegular is a regular message (0) that the customer sees.
	MessageVisibilityRegular MessageVisibility = 0
	// MessageVisibilityInternalNote is an internal note (1) that only staff see.
	MessageVisibilityInternalNote MessageVisibility = 1
	// MessageVisibilityCollisionDetected is Re:amaze's collision-detected message (2), listed but never written.
	MessageVisibilityCollisionDetected MessageVisibility = 2
)

// MessageOrigin is Re:amaze's own integer for where a message originated, such as 1 for email
// or 7 for the API. Results pass through every value; GET /messages documents the full list.
type MessageOrigin int

// ChannelType is Re:amaze's own integer channel type, such as 1 for email or 6 for chat.
// Results pass through every value; GET /channels documents the full list.
type ChannelType int

// Conversation is the connector's view of one Re:amaze conversation. Custom data attributes,
// followers, and attachments are omitted.
type Conversation struct {
	// ID is the conversation's slug, which Re:amaze documents as its unique identifier and uses
	// in every conversation URL, such as knock-knock.
	ID string `json:"id"`
	// Subject is the conversation subject.
	Subject string `json:"subject,omitempty"`
	// Status is Re:amaze's integer status.
	Status ConversationStatus `json:"status"`
	// Tags lists the conversation's tags, Re:amaze's tag_list.
	Tags []string `json:"tags"`
	// Channel is the channel the conversation belongs to, which Re:amaze calls its category.
	Channel ConversationChannel `json:"channel"`
	// Requester is the customer who started the conversation, Re:amaze's author, or nil when absent.
	Requester *ConversationParticipant `json:"requester,omitempty"`
	// Assignee is the assigned staff user, or nil when the conversation is unassigned.
	Assignee *ConversationParticipant `json:"assignee,omitempty"`
	// FirstMessage is the conversation's first message as plain or Markdown text. searchConversations
	// leaves it empty to keep pages small.
	FirstMessage string `json:"firstMessage,omitempty"`
	// IsFirstMessageTruncated reports that FirstMessage was cut at MaxTextBytes.
	IsFirstMessageTruncated bool `json:"isFirstMessageTruncated,omitempty"`
	// CreatedAt is when the conversation was created.
	CreatedAt time.Time `json:"createdAt"`
	// LastCustomerMessageAt is when the customer last wrote, or nil. It is the clock for a
	// first- or next-response target, because Re:amaze exposes no SLA due time.
	LastCustomerMessageAt *time.Time `json:"lastCustomerMessageAt,omitempty"`
	// LastStaffMessageAt is when staff last wrote, or nil. Re:amaze documents it on single
	// conversation reads, so searchConversations usually leaves it nil.
	LastStaffMessageAt *time.Time `json:"lastStaffMessageAt,omitempty"`
}

// ConversationChannel is the Re:amaze channel, which Re:amaze calls a category.
type ConversationChannel struct {
	// Slug identifies the channel in API requests, such as support.
	Slug string `json:"slug"`
	// Name is the channel's display name.
	Name string `json:"name,omitempty"`
	// Type is Re:amaze's integer channel type, such as 1 for email.
	Type ChannelType `json:"type,omitempty"`
}

// ConversationParticipant is a customer or staff user as Re:amaze embeds them.
type ConversationParticipant struct {
	// Name is the user's name.
	Name string `json:"name,omitempty"`
	// Email is the user's email address, or empty for a contact without one, such as an SMS customer.
	Email string `json:"email,omitempty"`
}

// ConversationMessage is one message or internal note in a conversation.
type ConversationMessage struct {
	// Body is the message as Re:amaze returns it, plain or Markdown text.
	Body string `json:"body"`
	// IsBodyTruncated reports that Body was cut at MaxTextBytes.
	IsBodyTruncated bool `json:"isBodyTruncated,omitempty"`
	// Visibility is Re:amaze's integer visibility.
	Visibility MessageVisibility `json:"visibility"`
	// IsInternalNote is true for an internal note, visibility 1, that only staff see.
	IsInternalNote bool `json:"isInternalNote"`
	// Origin is Re:amaze's integer origin, such as 1 for email or 7 for the API.
	Origin MessageOrigin `json:"origin"`
	// OriginID is the message's unique origin identifier, such as the one replyToConversation sets.
	OriginID string `json:"originId,omitempty"`
	// Author is the staff user or customer who wrote the message, or nil when absent.
	Author *ConversationParticipant `json:"author,omitempty"`
	// CreatedAt is when the message was created.
	CreatedAt time.Time `json:"createdAt"`
}

// MaxTextBytes bounds each first message and message body a Result carries, so durable Step
// state stays small. Longer text is cut at a UTF-8 boundary and flagged.
const MaxTextBytes = 16 << 10

const (
	maximumOriginIDLength = 256
	maximumTagLength      = 64
)

var (
	// conversationIDPattern accepts a slug or a database ID; neither can change the request path.
	conversationIDPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9_-]{0,254})$`)
	channelSlugPattern    = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9_-]{0,127})$`)
	// tagPattern rejects commas, which separate tags in Re:amaze's tag filter.
	tagPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9 _./+:#&-]{0,62}[A-Za-z0-9_./+:#&-])?$`)
)

// conversationWire is the conversation JSON the connector reads; Re:amaze's nulls decode as zero values.
type conversationWire struct {
	Slug                string                   `json:"slug"`
	Subject             string                   `json:"subject"`
	Status              *int                     `json:"status"`
	CreatedAt           string                   `json:"created_at"`
	TagList             []string                 `json:"tag_list"`
	Message             *messageBodyWire         `json:"message"`
	LastCustomerMessage *messageBodyWire         `json:"last_customer_message"`
	LastStaffMessage    *messageBodyWire         `json:"last_staff_message"`
	Author              *participantWire         `json:"author"`
	Assignee            *participantWire         `json:"assignee"`
	Category            *conversationChannelWire `json:"category"`
	// Data holds custom attributes; only the connector's dispatch key is ever read from it.
	Data map[string]json.RawMessage `json:"data"`
}

type messageBodyWire struct {
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

type participantWire struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type conversationChannelWire struct {
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	Channel int    `json:"channel"`
}

type messageWire struct {
	Body       string           `json:"body"`
	Visibility *int             `json:"visibility"`
	Origin     int              `json:"origin"`
	OriginID   json.RawMessage  `json:"origin_id"`
	CreatedAt  string           `json:"created_at"`
	User       *participantWire `json:"user"`
}

// pageWire is the pagination envelope Re:amaze returns around every list.
type pageWire struct {
	PageSize   *int `json:"page_size"`
	PageCount  *int `json:"page_count"`
	TotalCount *int `json:"total_count"`
}

// maximumListPageSize bounds a decoded list page; Re:amaze documents 30 entries per page.
const maximumListPageSize = 100

// decodeConversationBody decodes the bare conversation object Re:amaze returns from reads and writes.
func decodeConversationBody(body []byte, expectedConversationID string) (Conversation, error) {
	var wire conversationWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return Conversation{}, errors.New("response is not a conversation object")
	}
	conversation, err := decodeConversationWire(wire, true)
	if err != nil {
		return Conversation{}, err
	}
	if expectedConversationID != "" && conversation.ID != expectedConversationID {
		return Conversation{}, errors.New("response is for another conversation")
	}
	return conversation, nil
}

// decodeConversationWire validates one conversation and converts Re:amaze's snake_case fields.
func decodeConversationWire(wire conversationWire, isFirstMessageIncluded bool) (Conversation, error) {
	if validateConversationID("slug", wire.Slug) != nil {
		return Conversation{}, errors.New("conversation slug is missing or invalid")
	}
	if wire.Status == nil {
		return Conversation{}, errors.New("conversation status is missing")
	}
	createdAt, err := time.Parse(time.RFC3339, wire.CreatedAt)
	if err != nil {
		return Conversation{}, errors.New("conversation created_at is not ISO 8601")
	}
	conversation := Conversation{
		ID: wire.Slug, Subject: wire.Subject, Status: ConversationStatus(*wire.Status), Tags: append([]string{}, wire.TagList...),
		Requester: decodeParticipant(wire.Author), Assignee: decodeParticipant(wire.Assignee), CreatedAt: createdAt.UTC(),
	}
	if wire.Category != nil {
		conversation.Channel = ConversationChannel{Slug: wire.Category.Slug, Name: wire.Category.Name, Type: ChannelType(wire.Category.Channel)}
	}
	if isFirstMessageIncluded && wire.Message != nil {
		conversation.FirstMessage, conversation.IsFirstMessageTruncated = truncateUTF8(wire.Message.Body, MaxTextBytes)
	}
	if conversation.LastCustomerMessageAt, err = parseMessageTime("last_customer_message", wire.LastCustomerMessage); err != nil {
		return Conversation{}, err
	}
	if conversation.LastStaffMessageAt, err = parseMessageTime("last_staff_message", wire.LastStaffMessage); err != nil {
		return Conversation{}, err
	}
	return conversation, nil
}

// decodeMessagePage decodes one page of a conversation's messages, which Re:amaze sorts newest first.
func decodeMessagePage(body []byte) ([]ConversationMessage, pageWire, error) {
	var document struct {
		pageWire
		Messages *[]messageWire `json:"messages"`
	}
	if err := json.Unmarshal(body, &document); err != nil || document.Messages == nil {
		return nil, pageWire{}, errors.New("response is not a message page")
	}
	if err := document.pageWire.validate(len(*document.Messages)); err != nil {
		return nil, pageWire{}, fmt.Errorf("message page: %w", err)
	}
	messages := make([]ConversationMessage, 0, len(*document.Messages))
	for index, wire := range *document.Messages {
		message, err := decodeMessageWire(wire)
		if err != nil {
			return nil, pageWire{}, fmt.Errorf("message %d: %w", index, err)
		}
		messages = append(messages, message)
	}
	return messages, document.pageWire, nil
}

func decodeMessageWire(wire messageWire) (ConversationMessage, error) {
	if wire.Visibility == nil {
		return ConversationMessage{}, errors.New("message visibility is missing")
	}
	createdAt, err := time.Parse(time.RFC3339, wire.CreatedAt)
	if err != nil {
		return ConversationMessage{}, errors.New("message created_at is not ISO 8601")
	}
	originID, err := decodeOriginID(wire.OriginID)
	if err != nil {
		return ConversationMessage{}, err
	}
	body, isBodyTruncated := truncateUTF8(wire.Body, MaxTextBytes)
	visibility := MessageVisibility(*wire.Visibility)
	return ConversationMessage{
		Body: body, IsBodyTruncated: isBodyTruncated, Visibility: visibility, IsInternalNote: visibility == MessageVisibilityInternalNote,
		Origin: MessageOrigin(wire.Origin), OriginID: originID, Author: decodeParticipant(wire.User), CreatedAt: createdAt.UTC(),
	}, nil
}

// validate checks the pagination counters every Re:amaze list carries.
func (page pageWire) validate(entryCount int) error {
	if page.PageCount == nil || page.TotalCount == nil || *page.PageCount < 0 || *page.TotalCount < 0 {
		return errors.New("page_count and total_count are required")
	}
	if entryCount > maximumListPageSize || (page.PageSize != nil && entryCount > max(*page.PageSize, 0)) {
		return errors.New("the page holds more entries than its page size")
	}
	return nil
}

// decodeOriginID accepts a string or number, because Re:amaze shows both shapes, and bounds it.
func decodeOriginID(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		var number json.Number
		if json.Unmarshal(raw, &number) != nil {
			return "", errors.New("message origin_id is neither a string nor a number")
		}
		text = number.String()
	}
	if len(text) > maximumOriginIDLength || !utf8.ValidString(text) {
		return "", errors.New("message origin_id is too long")
	}
	return text, nil
}

func decodeParticipant(wire *participantWire) *ConversationParticipant {
	if wire == nil || (wire.Name == "" && wire.Email == "") {
		return nil
	}
	return &ConversationParticipant{Name: wire.Name, Email: wire.Email}
}

func parseMessageTime(fieldName string, wire *messageBodyWire) (*time.Time, error) {
	if wire == nil || wire.CreatedAt == "" {
		return nil, nil
	}
	instant, err := time.Parse(time.RFC3339, wire.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("conversation %s.created_at is not ISO 8601", fieldName)
	}
	instant = instant.UTC()
	return &instant, nil
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

func validateConversationID(fieldName string, conversationID string) error {
	if !conversationIDPattern.MatchString(conversationID) {
		return fmt.Errorf("%s must be a Re:amaze conversation slug of letters, digits, hyphens, and underscores, such as knock-knock", fieldName)
	}
	return nil
}

func validateChannelSlug(fieldName string, slug string) error {
	if !channelSlugPattern.MatchString(slug) {
		return fmt.Errorf("%s must be a Re:amaze channel slug of letters, digits, hyphens, and underscores, such as support", fieldName)
	}
	return nil
}

// validateConversationStatus accepts only the statuses Re:amaze documents, 0 through 9.
func validateConversationStatus(fieldName string, status ConversationStatus) error {
	if status < ConversationStatusOpen || status > ConversationStatusAISpam {
		return fmt.Errorf("%s %d is not a Re:amaze status; use 0 (Open), 1 (Responded), 2 (Done), 3 (Spam), 4 (Archived), 5 (On Hold), 6 (Auto-Done), 7 (AI Agent Assigned), 8 (AI Agent Done), or 9 (Spam identified by AI)", fieldName, status)
	}
	return nil
}

// validateHoldUntil requires an RFC 3339 instant, and only alongside status 5 (On Hold).
func validateHoldUntil(holdUntil string, status *ConversationStatus) error {
	if holdUntil == "" {
		return nil
	}
	if status == nil || *status != ConversationStatusOnHold {
		return errors.New("holdUntil requires status 5 (On Hold)")
	}
	if _, err := time.Parse(time.RFC3339, holdUntil); err != nil {
		return errors.New("holdUntil must be an RFC 3339 instant such as 2026-07-15T09:00:00Z")
	}
	return nil
}

func validateTags(fieldName string, tags []string) error {
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if len(tag) > maximumTagLength || !tagPattern.MatchString(tag) {
			return fmt.Errorf("%s entry %q must be 1 to 64 letters, digits, spaces, and _ . / + : # & - characters without commas, starting with a letter or digit", fieldName, tag)
		}
		folded := strings.ToLower(tag)
		if seen[folded] {
			return fmt.Errorf("%s lists %q twice", fieldName, tag)
		}
		seen[folded] = true
	}
	return nil
}

func validateTextInput(fieldName string, text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("%s is required", fieldName)
	}
	if len(text) > MaxTextBytes || !utf8.ValidString(text) {
		return fmt.Errorf("%s must be UTF-8 text of at most %d bytes", fieldName, MaxTextBytes)
	}
	return nil
}
