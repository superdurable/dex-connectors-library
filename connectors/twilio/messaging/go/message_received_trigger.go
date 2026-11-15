// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package messaging

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// InboundMessageDirection is Twilio's direction value for a message the account received.
const InboundMessageDirection = "inbound"

const (
	// MessageStatusReceiving means Twilio received an inbound message and is still processing it, such as
	// fetching MMS media. The messageReceived Trigger waits for received, at most receivingHoldLimit.
	MessageStatusReceiving MessageStatus = "receiving"
	// MessageStatusReceived means Twilio finished processing an inbound message.
	MessageStatusReceived MessageStatus = "received"
)

// MessageReceivedTriggerConfiguration is the messageReceived binding configuration that the
// receivingAddressInput unit saves in Dex Web. Changing it for a binding that already polled makes its next
// poll record a new baseline.
type MessageReceivedTriggerConfiguration struct {
	// To is the receiving Twilio number in E.164 form, such as +14155550100, or a WhatsApp sender written
	// whatsapp:+14155550100. Blank receives every inbound message of the connection's account.
	To string `json:"to,omitempty"`
}

// Validate accepts a blank To, an E.164 number, or a whatsapp:+E.164 address without surrounding spaces.
func (configuration MessageReceivedTriggerConfiguration) Validate() error {
	switch {
	case configuration.To == "":
		return nil
	case configuration.To != strings.TrimSpace(configuration.To):
		return errors.New("messageReceived to must not have surrounding whitespace")
	case phoneNumberPattern.MatchString(configuration.To), isWhatsAppAddress(configuration.To):
		return nil
	default:
		return errors.New("messageReceived to must be an E.164 phone number or a whatsapp:+E.164 address")
	}
}

// MessageReceivedEvent is one inbound message read from the Twilio Messages list.
//
// Unlike operation results, it carries the message text, because acting on an inbound message needs it.
// Media URLs, Twilio error text, and credentials are never included. The Trigger event ID is
// "twilio-message-" followed by the message SID.
type MessageReceivedEvent struct {
	// Message is the safe message view: SID, account, sender (From), receiving number (To), status
	// (received, or receiving when processing outlasted the hold limit), direction, segment and media
	// counts, and timestamps. SentAt is when Twilio received the message.
	Message Message `json:"message"`
	// Body is the inbound text, empty for a media-only MMS; Twilio may redact it per the account's
	// retention settings.
	Body string `json:"body,omitempty"`
}

// newMessageReceivedTriggerEvent converts one inbound message. A message SID is assigned once, so the ID
// is a pure function of the message and a refetch after a crash yields the same ID.
func newMessageReceivedTriggerEvent(message Message, body string, now time.Time) sdkgo.TriggerEvent[MessageReceivedEvent] {
	occurredAt := message.SentAt
	if occurredAt.IsZero() {
		occurredAt = message.CreatedAt
	}
	if occurredAt.IsZero() {
		occurredAt = now.UTC()
	}
	return sdkgo.TriggerEvent[MessageReceivedEvent]{
		ID:         messageReceivedEventID(message.SID),
		OccurredAt: occurredAt,
		Payload:    MessageReceivedEvent{Message: message, Body: body},
	}
}

func messageReceivedEventID(messageSID string) string {
	return "twilio-message-" + messageSID
}

func messageReceivedTriggerConfigurationError(err error) error {
	return fmt.Errorf("twilio-messaging messageReceived configuration: %w", err)
}
