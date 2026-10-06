// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// gorgiasTicketWire is the ticket JSON the connector reads from full tickets and list items alike.
type gorgiasTicketWire struct {
	ID                          int64                `json:"id"`
	Subject                     *string              `json:"subject"`
	Status                      string               `json:"status"`
	Priority                    *string              `json:"priority"`
	Channel                     *string              `json:"channel"`
	Via                         *string              `json:"via"`
	ExternalID                  *string              `json:"external_id"`
	Language                    *string              `json:"language"`
	Spam                        *bool                `json:"spam"`
	Customer                    *gorgiasCustomerWire `json:"customer"`
	AssigneeUser                *gorgiasIDWire       `json:"assignee_user"`
	AssigneeTeam                *gorgiasIDWire       `json:"assignee_team"`
	Tags                        []gorgiasTagWire     `json:"tags"`
	Messages                    []gorgiasMessageWire `json:"messages"`
	MessagesCount               *int                 `json:"messages_count"`
	CreatedDatetime             string               `json:"created_datetime"`
	UpdatedDatetime             *string              `json:"updated_datetime"`
	LastReceivedMessageDatetime *string              `json:"last_received_message_datetime"`
	LastMessageDatetime         *string              `json:"last_message_datetime"`
	ClosedDatetime              *string              `json:"closed_datetime"`
	SnoozeDatetime              *string              `json:"snooze_datetime"`
	TrashedDatetime             *string              `json:"trashed_datetime"`
}

type gorgiasIDWire struct {
	ID int64 `json:"id"`
}

type gorgiasTagWire struct {
	Name string `json:"name"`
}

type gorgiasCustomerWire struct {
	ID         int64   `json:"id"`
	Email      *string `json:"email"`
	Name       *string `json:"name"`
	ExternalID *string `json:"external_id"`
	Language   *string `json:"language"`
}

type gorgiasAddressWire struct {
	Address string `json:"address"`
}

type gorgiasMessageWire struct {
	ID           int64          `json:"id"`
	TicketID     int64          `json:"ticket_id"`
	Channel      string         `json:"channel"`
	Via          *string        `json:"via"`
	Public       *bool          `json:"public"`
	FromAgent    bool           `json:"from_agent"`
	Sender       *gorgiasIDWire `json:"sender"`
	Subject      *string        `json:"subject"`
	BodyText     *string        `json:"body_text"`
	StrippedText *string        `json:"stripped_text"`
	ExternalID   *string        `json:"external_id"`
	// IntegrationID is the Gorgias integration that received or sent the message.
	IntegrationID *int64 `json:"integration_id"`
	Source        *struct {
		From *gorgiasAddressWire  `json:"from"`
		To   []gorgiasAddressWire `json:"to"`
		CC   []gorgiasAddressWire `json:"cc"`
	} `json:"source"`
	CreatedDatetime string  `json:"created_datetime"`
	SentDatetime    *string `json:"sent_datetime"`
	FailedDatetime  *string `json:"failed_datetime"`
}

// gorgiasListWire is Gorgias's cursor-paginated list envelope.
type gorgiasListWire[T any] struct {
	Data *[]T `json:"data"`
	Meta *struct {
		NextCursor *string `json:"next_cursor"`
	} `json:"meta"`
}

// decodeListBody decodes a list envelope, keeping nil when Gorgias omitted the data array.
func decodeListBody[T any](body []byte, name string) ([]T, string, error) {
	var wire gorgiasListWire[T]
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, "", fmt.Errorf("%s page is not a Gorgias list", name)
	}
	if wire.Data == nil {
		return nil, "", fmt.Errorf("%s page has no data array", name)
	}
	nextCursor := ""
	if wire.Meta != nil && wire.Meta.NextCursor != nil {
		nextCursor = *wire.Meta.NextCursor
		if nextCursor != "" && !isValidCursor(nextCursor) {
			return nil, "", fmt.Errorf("%s page has an invalid next cursor", name)
		}
	}
	return *wire.Data, nextCursor, nil
}

// decodeTicketWire validates one ticket and converts Gorgias's snake_case fields.
func decodeTicketWire(wire gorgiasTicketWire) (Ticket, error) {
	if wire.ID < 1 {
		return Ticket{}, errors.New("ticket ID is missing")
	}
	if wire.Status == "" {
		return Ticket{}, errors.New("ticket status is missing")
	}
	createdAt, err := parseGorgiasTimestamp(wire.CreatedDatetime)
	if err != nil {
		return Ticket{}, errors.New("ticket created_datetime is not an ISO 8601 timestamp")
	}
	ticket := Ticket{
		ID: wire.ID, Subject: stringValue(wire.Subject), Status: TicketStatus(wire.Status), Priority: TicketPriority(stringValue(wire.Priority)),
		Channel: stringValue(wire.Channel), Via: stringValue(wire.Via), ExternalID: stringValue(wire.ExternalID),
		Language: stringValue(wire.Language), IsSpam: wire.Spam != nil && *wire.Spam, CreatedAt: createdAt, UpdatedAt: createdAt,
		Tags: make([]string, 0, len(wire.Tags)),
	}
	if wire.Customer != nil {
		ticket.RequesterID, ticket.RequesterEmail = wire.Customer.ID, stringValue(wire.Customer.Email)
	}
	if wire.AssigneeUser != nil {
		ticket.AssigneeUserID = wire.AssigneeUser.ID
	}
	if wire.AssigneeTeam != nil {
		ticket.AssigneeTeamID = wire.AssigneeTeam.ID
	}
	if wire.MessagesCount != nil {
		ticket.MessageCount = *wire.MessagesCount
	}
	for _, tag := range wire.Tags {
		if tag.Name == "" {
			return Ticket{}, errors.New("ticket tag has no name")
		}
		ticket.Tags = append(ticket.Tags, tag.Name)
	}
	if updatedAt, err := parseOptionalTimestamp("ticket updated_datetime", wire.UpdatedDatetime); err != nil {
		return Ticket{}, err
	} else if updatedAt != nil {
		ticket.UpdatedAt = *updatedAt
	}
	for _, field := range []struct {
		name   string
		value  *string
		target **time.Time
	}{
		{"ticket last_received_message_datetime", wire.LastReceivedMessageDatetime, &ticket.LastReceivedMessageAt},
		{"ticket last_message_datetime", wire.LastMessageDatetime, &ticket.LastMessageAt},
		{"ticket closed_datetime", wire.ClosedDatetime, &ticket.ClosedAt},
		{"ticket snooze_datetime", wire.SnoozeDatetime, &ticket.SnoozedUntil},
		{"ticket trashed_datetime", wire.TrashedDatetime, &ticket.TrashedAt},
	} {
		if *field.target, err = parseOptionalTimestamp(field.name, field.value); err != nil {
			return Ticket{}, err
		}
	}
	return ticket, nil
}

// decodeTicketBody decodes a ticket object and returns its wire form for the embedded customer and messages.
func decodeTicketBody(body []byte, expectedTicketID int64) (Ticket, gorgiasTicketWire, error) {
	var wire gorgiasTicketWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return Ticket{}, gorgiasTicketWire{}, errors.New("ticket response is not a ticket object")
	}
	ticket, err := decodeTicketWire(wire)
	if err != nil {
		return Ticket{}, gorgiasTicketWire{}, err
	}
	if expectedTicketID > 0 && ticket.ID != expectedTicketID {
		return Ticket{}, gorgiasTicketWire{}, errors.New("ticket response is for another ticket")
	}
	return ticket, wire, nil
}

func decodeMessageWire(wire gorgiasMessageWire) (TicketMessage, error) {
	if wire.ID < 1 {
		return TicketMessage{}, errors.New("message ID is missing")
	}
	createdAt, err := parseGorgiasTimestamp(wire.CreatedDatetime)
	if err != nil {
		return TicketMessage{}, errors.New("message created_datetime is not an ISO 8601 timestamp")
	}
	text := stringValue(wire.BodyText)
	if text == "" {
		text = stringValue(wire.StrippedText)
	}
	body, isBodyTruncated := truncateUTF8(text, MaxTextBytes)
	message := TicketMessage{
		ID: wire.ID, TicketID: wire.TicketID, Channel: wire.Channel, Via: stringValue(wire.Via),
		IsPublic: wire.Channel != MessageChannelInternalNote && (wire.Public == nil || *wire.Public), IsFromAgent: wire.FromAgent,
		Subject: stringValue(wire.Subject), Body: body, IsBodyTruncated: isBodyTruncated, ExternalID: stringValue(wire.ExternalID),
		CreatedAt: createdAt,
	}
	if wire.Sender != nil {
		message.SenderID = wire.Sender.ID
	}
	if message.SentAt, err = parseOptionalTimestamp("message sent_datetime", wire.SentDatetime); err != nil {
		return TicketMessage{}, err
	}
	if message.FailedAt, err = parseOptionalTimestamp("message failed_datetime", wire.FailedDatetime); err != nil {
		return TicketMessage{}, err
	}
	return message, nil
}

func decodeCustomerWire(wire gorgiasCustomerWire) (Customer, error) {
	if wire.ID < 1 {
		return Customer{}, errors.New("customer ID is missing")
	}
	return Customer{
		ID: wire.ID, Email: stringValue(wire.Email), Name: stringValue(wire.Name),
		ExternalID: stringValue(wire.ExternalID), Language: stringValue(wire.Language),
	}, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
