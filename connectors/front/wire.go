// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	conversationIDPattern = regexp.MustCompile(`^cnv_[A-Za-z0-9]{1,40}$`)
	teammateIDPattern     = regexp.MustCompile(`^tea_[A-Za-z0-9]{1,40}$`)
	tagIDPattern          = regexp.MustCompile(`^tag_[A-Za-z0-9]{1,40}$`)
	inboxIDPattern        = regexp.MustCompile(`^inb_[A-Za-z0-9]{1,40}$`)
	statusIDPattern       = regexp.MustCompile(`^sts_[A-Za-z0-9]{1,40}$`)
	contactIDPattern      = regexp.MustCompile(`^crd_[A-Za-z0-9]{1,40}$`)
	messageIDPattern      = regexp.MustCompile(`^msg_[A-Za-z0-9]{1,40}$`)
	commentIDPattern      = regexp.MustCompile(`^com_[A-Za-z0-9]{1,40}$`)
	pageTokenPattern      = regexp.MustCompile(`^[A-Za-z0-9._~-]{1,1000}$`)
	companyAPIHostPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.api\.frontapp\.com$`)
)

// frontTimestamp decodes Front's Unix seconds with millisecond fractions; null and absent decode as zero.
type frontTimestamp struct{ instant time.Time }

// UnmarshalJSON reads a non-negative number of seconds, keeping milliseconds, or null.
func (timestamp *frontTimestamp) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	seconds, err := strconv.ParseFloat(string(data), 64)
	if err != nil || seconds < 0 || seconds > 1e11 || math.IsNaN(seconds) {
		return errors.New("a Front timestamp is not Unix seconds")
	}
	if seconds > 0 {
		timestamp.instant = time.UnixMilli(int64(math.Round(seconds * 1000))).UTC()
	}
	return nil
}

func (timestamp frontTimestamp) pointer() *time.Time {
	if timestamp.instant.IsZero() {
		return nil
	}
	instant := timestamp.instant
	return &instant
}

type frontLinksWire struct {
	Related struct {
		Contact string `json:"contact"`
	} `json:"related"`
}

type frontTeammateWire struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type frontRecipientWire struct {
	Links  frontLinksWire `json:"_links"`
	Handle string         `json:"handle"`
	Role   string         `json:"role"`
	Name   string         `json:"name"`
}

type frontConversationWire struct {
	ID             string              `json:"id"`
	Type           string              `json:"type"`
	Subject        string              `json:"subject"`
	Status         string              `json:"status"`
	StatusID       string              `json:"status_id"`
	StatusCategory string              `json:"status_category"`
	TicketIDs      []string            `json:"ticket_ids"`
	Assignee       *frontTeammateWire  `json:"assignee"`
	Recipient      *frontRecipientWire `json:"recipient"`
	Tags           []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"tags"`
	IsPrivate          bool              `json:"is_private"`
	ScheduledReminders []json.RawMessage `json:"scheduled_reminders"`
	CreatedAt          frontTimestamp    `json:"created_at"`
	UpdatedAt          frontTimestamp    `json:"updated_at"`
	WaitingSince       frontTimestamp    `json:"waiting_since"`
	DueAt              frontTimestamp    `json:"due_at"`
}

// frontPageWire is a Core API list: _results with an optional _pagination.next link and search _total.
type frontPageWire[T any] struct {
	Pagination struct {
		Next *string `json:"next"`
	} `json:"_pagination"`
	Total   *int `json:"_total"`
	Results []T  `json:"_results"`
}

func decodeConversation(body []byte) (Conversation, error) {
	var wire frontConversationWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return Conversation{}, errors.New("the conversation is not valid JSON")
	}
	return convertConversation(wire)
}

func convertConversation(wire frontConversationWire) (Conversation, error) {
	if !conversationIDPattern.MatchString(wire.ID) || wire.Status == "" {
		return Conversation{}, errors.New("a conversation lacks a valid ID or status")
	}
	conversation := Conversation{
		ID: wire.ID, Type: wire.Type, Subject: wire.Subject, Status: ConversationStatus(wire.Status),
		StatusCategory: ConversationStatusCategory(wire.StatusCategory), TicketIDs: wire.TicketIDs,
		IsPrivate: wire.IsPrivate, CreatedAt: wire.CreatedAt.instant, UpdatedAt: wire.UpdatedAt.pointer(),
		WaitingSince: wire.WaitingSince.pointer(), DueAt: wire.DueAt.pointer(),
		IsSnoozed: wire.Status == string(ConversationStatusArchived) && len(wire.ScheduledReminders) > 0,
	}
	if wire.StatusID != "" {
		if !statusIDPattern.MatchString(wire.StatusID) {
			return Conversation{}, errors.New("a conversation has an invalid status_id")
		}
		conversation.StatusID = wire.StatusID
	}
	assignee, err := convertTeammate(wire.Assignee)
	if err != nil {
		return Conversation{}, err
	}
	conversation.Assignee = assignee
	if wire.Recipient != nil && wire.Recipient.Handle != "" {
		recipient := convertRecipient(*wire.Recipient)
		conversation.Recipient = &recipient
	}
	for _, tag := range wire.Tags {
		if !tagIDPattern.MatchString(tag.ID) {
			return Conversation{}, errors.New("a conversation tag lacks a valid ID")
		}
		conversation.Tags = append(conversation.Tags, Tag{ID: tag.ID, Name: tag.Name})
	}
	return conversation, nil
}

// convertTeammate returns nil for an absent teammate and rejects one without a valid ID.
func convertTeammate(wire *frontTeammateWire) (*Teammate, error) {
	if wire == nil || (wire.ID == "" && wire.Email == "") {
		return nil, nil
	}
	if !teammateIDPattern.MatchString(wire.ID) {
		return nil, errors.New("a teammate lacks a valid ID")
	}
	return &Teammate{ID: wire.ID, Email: wire.Email, FirstName: wire.FirstName, LastName: wire.LastName}, nil
}

func convertRecipient(wire frontRecipientWire) Recipient {
	return Recipient{Handle: wire.Handle, Role: wire.Role, Name: wire.Name, ContactID: lastPathSegment(wire.Links.Related.Contact, contactIDPattern)}
}

// lastPathSegment returns a resource link's final segment when it matches pattern, otherwise empty.
func lastPathSegment(link string, pattern *regexp.Regexp) string {
	parsed, err := url.Parse(strings.TrimSpace(link))
	if err != nil || parsed.Path == "" {
		return ""
	}
	if segment := path.Base(parsed.Path); pattern.MatchString(segment) {
		return segment
	}
	return ""
}

// mergedConversationID reads the absorbing conversation from a 301 Location header, or returns empty.
func mergedConversationID(location string) string {
	return lastPathSegment(location, conversationIDPattern)
}

// truncateUTF8 cuts value at limit bytes on a rune boundary and reports whether it cut.
func truncateUTF8(value string, limit int) (string, bool) {
	if len(value) <= limit {
		return value, false
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut], true
}

func validateResourceID(field string, value string, pattern *regexp.Regexp, example string) error {
	if !pattern.MatchString(value) {
		return fmt.Errorf("%s must be a Front ID such as %s", field, example)
	}
	return nil
}
