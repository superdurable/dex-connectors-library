// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// ConversationStatus is Hiver's own conversation status. The connector never maps it to
// another desk's vocabulary.
type ConversationStatus string

const (
	// ConversationStatusOpen is Hiver's open status.
	ConversationStatusOpen ConversationStatus = "open"
	// ConversationStatusPending is Hiver's pending status.
	ConversationStatusPending ConversationStatus = "pending"
	// ConversationStatusClosed is Hiver's closed status. The connector sends it to Hiver as
	// close, the value Hiver's update documents, and reports both close and closed as closed.
	ConversationStatusClosed ConversationStatus = "closed"
)

// ConversationStatuses returns the statuses updateConversation can set, in Hiver's order.
func ConversationStatuses() []ConversationStatus {
	return []ConversationStatus{ConversationStatusOpen, ConversationStatusPending, ConversationStatusClosed}
}

// Inbox is one Hiver shared inbox.
type Inbox struct {
	// ID is the Hiver inbox ID that every conversation operation takes.
	ID string `json:"id"`
	// DisplayName is the inbox name shown in Hiver, such as Customer Support.
	DisplayName string `json:"displayName"`
	// Email is the shared mailbox address, such as support@acme.example.com.
	Email string `json:"email"`
	// ChannelType is Hiver's channel type, such as email.
	ChannelType string `json:"channelType,omitempty"`
	// IsAuthorized reports that Hiver can still access the shared mailbox; false means an admin
	// must reauthorize it in Hiver before new email arrives.
	IsAuthorized bool `json:"isAuthorized"`
}

// Conversation is one Hiver conversation: a customer email thread in a shared inbox.
type Conversation struct {
	// ID is the Hiver conversation ID.
	ID string `json:"id"`
	// InboxID is the shared inbox the conversation was read from.
	InboxID string `json:"inboxId"`
	// Status is Hiver's status: open, pending, or closed, or another value Hiver returns unchanged.
	Status ConversationStatus `json:"status"`
	// Assignee is the Hiver user or team the conversation is assigned to, or nil when unassigned.
	Assignee *ConversationAssignee `json:"assignee,omitempty"`
	// TagIDs are the IDs of the inbox tags on the conversation.
	TagIDs []string `json:"tagIds"`
	// GmailThreadID is the Gmail thread ID of the shared mailbox's own user, which every
	// conversation operation also accepts in place of ID.
	GmailThreadID string `json:"gmailThreadId,omitempty"`
	// PrivatePermalink opens the conversation in Hiver for a signed-in Hiver user. Hiver's public
	// permalink, which anyone with the link can open, is deliberately not carried.
	PrivatePermalink string `json:"privatePermalink,omitempty"`
}

// ConversationAssignee is the owner of a conversation as Hiver reports it.
type ConversationAssignee struct {
	// Type is Hiver's assignee type, such as user.
	Type string `json:"type"`
	// ID is the Hiver user ID, or the ID of another assignee type.
	ID string `json:"id"`
}

// ConversationMessage identifies one email of a conversation.
type ConversationMessage struct {
	// HiverMessageID is Hiver's message ID, which createSharedDraft accepts.
	HiverMessageID string `json:"hiverMessageId,omitempty"`
	// GmailMessageID is the Gmail message ID of the shared mailbox's own user.
	GmailMessageID string `json:"gmailMessageId,omitempty"`
}

// ConversationDetails is one conversation with the IDs of its messages.
type ConversationDetails struct {
	// Conversation is the conversation Hiver returned.
	Conversation Conversation `json:"conversation"`
	// Messages are the conversation's message IDs in the order Hiver returns them; Hiver does
	// not document that order.
	Messages []ConversationMessage `json:"messages"`
}

// Tag is one tag of a Hiver shared inbox.
type Tag struct {
	// ID is the Hiver tag ID that conversations list in TagIDs.
	ID string `json:"id"`
	// Name is the tag name shown in Hiver.
	Name string `json:"name"`
}

var (
	statusTokenPattern     = regexp.MustCompile(`^[A-Za-z0-9 _-]{1,64}$`)
	assigneeTypePattern    = regexp.MustCompile(`^[a-z_]{1,32}$`)
	gmailIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)
)

// wireID accepts an ID that Hiver documents as a JSON string in some responses, a number in others.
type wireID string

// UnmarshalJSON accepts a JSON string, a non-negative integer, or null, which decodes as an empty ID.
func (id *wireID) UnmarshalJSON(raw []byte) error {
	trimmed := bytes.TrimSpace(raw)
	switch {
	case bytes.Equal(trimmed, []byte("null")):
		*id = ""
		return nil
	case len(trimmed) != 0 && trimmed[0] == '"':
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return err
		}
		*id = wireID(value)
		return nil
	}
	for _, character := range trimmed {
		if character < '0' || character > '9' {
			return errors.New("ID is neither a string nor an integer")
		}
	}
	*id = wireID(trimmed)
	return nil
}

type inboxWire struct {
	ID           wireID `json:"id"`
	DisplayName  string `json:"display_name"`
	ChannelType  string `json:"channel_type"`
	Email        string `json:"email"`
	IsAuthorised bool   `json:"is_authorised"`
}

type conversationWire struct {
	ID               wireID          `json:"id"`
	Assignee         *assigneeWire   `json:"assignee"`
	Status           *string         `json:"status"`
	TagIDs           []wireID        `json:"tag_ids"`
	GmailThreadID    string          `json:"gmail_thread_id"`
	PrivatePermalink string          `json:"private_permalink"`
	MessageIDs       []messageIDWire `json:"message_ids"`
}

type assigneeWire struct {
	Type string `json:"assignee_type"`
	ID   wireID `json:"assignee_id"`
}

type messageIDWire struct {
	HiverMessageID wireID `json:"hiver_message_id"`
	GmailMessageID string `json:"gmail_message_id"`
}

type tagWire struct {
	ID   wireID `json:"id"`
	Name string `json:"name"`
}

type inboxUserWire struct {
	ID    wireID `json:"id"`
	Email string `json:"email"`
}

// listPageWire is Hiver's list envelope: {"data":{"results":[...],"pagination":{"next_page":...}}}.
type listPageWire struct {
	Data *struct {
		Results    *[]json.RawMessage `json:"results"`
		Pagination *struct {
			NextPage *string `json:"next_page"`
		} `json:"pagination"`
	} `json:"data"`
}

// decodeListPage returns the raw results and the validated next-page token, empty after the last page.
func decodeListPage(body []byte) ([]json.RawMessage, string, error) {
	var page listPageWire
	if json.Unmarshal(body, &page) != nil || page.Data == nil || page.Data.Results == nil {
		return nil, "", errors.New("response is not a Hiver list page")
	}
	nextPageToken := ""
	if page.Data.Pagination != nil && page.Data.Pagination.NextPage != nil {
		nextPageToken = *page.Data.Pagination.NextPage
	}
	if nextPageToken != "" && !isPageToken(nextPageToken) {
		return nil, "", errors.New("next_page is not a page token")
	}
	return *page.Data.Results, nextPageToken, nil
}

// decodeSingleData returns the object in "data"; Hiver documents a conversation read as a one-element array.
func decodeSingleData(body []byte) (json.RawMessage, error) {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil || len(envelope.Data) == 0 {
		return nil, errors.New("response has no data object")
	}
	trimmed := bytes.TrimSpace(envelope.Data)
	switch {
	case len(trimmed) != 0 && trimmed[0] == '{':
		return trimmed, nil
	case len(trimmed) != 0 && trimmed[0] == '[':
		var elements []json.RawMessage
		if json.Unmarshal(trimmed, &elements) != nil || len(elements) != 1 {
			return nil, errors.New("data array does not hold exactly one object")
		}
		return elements[0], nil
	default:
		return nil, errors.New("data is neither an object nor an array")
	}
}

func decodeInbox(raw json.RawMessage) (Inbox, error) {
	var wire inboxWire
	if json.Unmarshal(raw, &wire) != nil {
		return Inbox{}, errors.New("inbox is not an object")
	}
	if !hiverIDPattern.MatchString(string(wire.ID)) {
		return Inbox{}, errors.New("inbox id is missing or invalid")
	}
	return Inbox{
		ID: string(wire.ID), DisplayName: wire.DisplayName, Email: strings.TrimSpace(wire.Email),
		ChannelType: wire.ChannelType, IsAuthorized: wire.IsAuthorised,
	}, nil
}

func decodeConversation(raw json.RawMessage, inboxID string) (ConversationDetails, error) {
	var wire conversationWire
	if json.Unmarshal(raw, &wire) != nil {
		return ConversationDetails{}, errors.New("conversation is not an object")
	}
	if !hiverIDPattern.MatchString(string(wire.ID)) {
		return ConversationDetails{}, errors.New("conversation id is missing or invalid")
	}
	if wire.Status == nil || !statusTokenPattern.MatchString(*wire.Status) {
		return ConversationDetails{}, fmt.Errorf("conversation %s status is missing or invalid", wire.ID)
	}
	conversation := Conversation{ID: string(wire.ID), InboxID: inboxID, Status: readConversationStatus(*wire.Status), TagIDs: []string{}}
	if wire.Assignee != nil && wire.Assignee.ID != "" {
		if !hiverIDPattern.MatchString(string(wire.Assignee.ID)) || !assigneeTypePattern.MatchString(wire.Assignee.Type) {
			return ConversationDetails{}, fmt.Errorf("conversation %s assignee is invalid", wire.ID)
		}
		conversation.Assignee = &ConversationAssignee{Type: wire.Assignee.Type, ID: string(wire.Assignee.ID)}
	}
	for _, tagID := range wire.TagIDs {
		if !hiverIDPattern.MatchString(string(tagID)) {
			return ConversationDetails{}, fmt.Errorf("conversation %s has an invalid tag ID", wire.ID)
		}
		conversation.TagIDs = append(conversation.TagIDs, string(tagID))
	}
	if wire.GmailThreadID != "" && !gmailIdentifierPattern.MatchString(wire.GmailThreadID) {
		return ConversationDetails{}, fmt.Errorf("conversation %s gmail_thread_id is invalid", wire.ID)
	}
	conversation.GmailThreadID = wire.GmailThreadID
	if wire.PrivatePermalink != "" {
		if !isHTTPSURL(wire.PrivatePermalink) {
			return ConversationDetails{}, fmt.Errorf("conversation %s private_permalink is not an HTTPS URL", wire.ID)
		}
		conversation.PrivatePermalink = wire.PrivatePermalink
	}
	messages := make([]ConversationMessage, 0, len(wire.MessageIDs))
	for _, message := range wire.MessageIDs {
		if (message.HiverMessageID != "" && !hiverIDPattern.MatchString(string(message.HiverMessageID))) ||
			(message.GmailMessageID != "" && !gmailIdentifierPattern.MatchString(message.GmailMessageID)) ||
			(message.HiverMessageID == "" && message.GmailMessageID == "") {
			return ConversationDetails{}, fmt.Errorf("conversation %s has an invalid message ID", wire.ID)
		}
		messages = append(messages, ConversationMessage{HiverMessageID: string(message.HiverMessageID), GmailMessageID: message.GmailMessageID})
	}
	return ConversationDetails{Conversation: conversation, Messages: messages}, nil
}

// readConversationStatus reports Hiver's close or closed as closed and keeps every other value.
func readConversationStatus(value string) ConversationStatus {
	if value == "close" || value == "closed" {
		return ConversationStatusClosed
	}
	return ConversationStatus(value)
}

// writeConversationStatus is the value Hiver's update documents for each status.
func writeConversationStatus(status ConversationStatus) string {
	if status == ConversationStatusClosed {
		return "close"
	}
	return string(status)
}

func decodeTag(raw json.RawMessage) (Tag, error) {
	var wire tagWire
	if json.Unmarshal(raw, &wire) != nil || !hiverIDPattern.MatchString(string(wire.ID)) {
		return Tag{}, errors.New("tag id is missing or invalid")
	}
	return Tag{ID: string(wire.ID), Name: wire.Name}, nil
}

func isHTTPSURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && len(value) <= 2048
}
