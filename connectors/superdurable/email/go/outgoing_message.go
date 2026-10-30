// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-message/mail"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// MaxRecipients bounds the To, Cc, and Bcc recipients of one message together.
	MaxRecipients = 50
	// MaxSendTextBytes bounds the plain-text body of one sent message or reply.
	MaxSendTextBytes = 512 << 10

	// messageIDPrefix marks Message-IDs this connector derives from a Dex idempotency key.
	messageIDPrefix = "dex-"
)

// SentMessage is the message an SMTP server accepted, or, on uncertain, the message that may have been sent.
type SentMessage struct {
	// MessageID is the Message-ID without angle brackets. It is derived from the Step's idempotency key, so
	// every attempt of one Step execution uses the same value, and an uncertain Result names the message to
	// look for in the recipient's or the Sent mailbox.
	MessageID string `json:"messageId"`
	// From is the sender address used in the From header and the SMTP envelope.
	From string `json:"from"`
	// Recipients lists the envelope recipients in To, Cc, then Bcc order.
	Recipients []string `json:"recipients"`
	// Subject is the subject that was sent, including a reply's Re: prefix.
	Subject string `json:"subject"`
	// InReplyTo is the replied-to Message-ID without angle brackets, or empty for a new message.
	InReplyTo string `json:"inReplyTo,omitempty"`
	// References lists the References header identifiers of a reply, oldest first.
	References []string `json:"references,omitempty"`
	// SentAt is the Date header of the submitted message, or zero when an earlier attempt submitted it.
	SentAt time.Time `json:"sentAt,omitempty"`
}

// messageSender is the From address and display name of every message a connection sends.
type messageSender struct {
	address string
	name    string
}

// outgoingMessage is one validated plain-text message ready to render.
type outgoingMessage struct {
	fromAddress string
	fromName    string
	to          []string
	cc          []string
	bcc         []string
	replyTo     string
	subject     string
	text        string
	messageID   string
	inReplyTo   string
	references  []string
	date        time.Time
}

// deriveMessageID builds the stable Message-ID for one Step execution from its idempotency key and the sender's domain.
func deriveMessageID(key sdkgo.IdempotencyKey, sender string) (string, error) {
	keyText := string(key)
	if keyText == "" || len(keyText) > 128 {
		return "", errors.New("the idempotency key cannot form a Message-ID")
	}
	for index := 0; index < len(keyText); index++ {
		character := keyText[index]
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-') {
			return "", errors.New("the idempotency key cannot form a Message-ID")
		}
	}
	domain := sender[strings.LastIndexByte(sender, '@')+1:]
	return messageIDPrefix + strings.ToLower(keyText) + "@" + strings.ToLower(domain), nil
}

// envelopeRecipients returns To, Cc, and Bcc in order without case-insensitive duplicates.
func (message outgoingMessage) envelopeRecipients() []string {
	seen := map[string]bool{}
	var recipients []string
	for _, group := range [][]string{message.to, message.cc, message.bcc} {
		for _, address := range group {
			canonical := strings.ToLower(address)
			if seen[canonical] {
				continue
			}
			seen[canonical] = true
			recipients = append(recipients, address)
		}
	}
	return recipients
}

// render writes RFC 5322 headers and a quoted-printable UTF-8 text body. Bcc recipients never appear in a header.
func (message outgoingMessage) render() ([]byte, error) {
	var header mail.Header
	header.SetDate(message.date)
	header.SetAddressList("From", []*mail.Address{{Name: message.fromName, Address: message.fromAddress}})
	header.SetAddressList("To", buildAddressList(message.to))
	if len(message.cc) > 0 {
		header.SetAddressList("Cc", buildAddressList(message.cc))
	}
	if message.replyTo != "" {
		header.SetAddressList("Reply-To", buildAddressList([]string{message.replyTo}))
	}
	header.SetSubject(message.subject)
	header.SetMessageID(message.messageID)
	if message.inReplyTo != "" {
		header.SetMsgIDList("In-Reply-To", []string{message.inReplyTo})
	}
	if len(message.references) > 0 {
		header.SetMsgIDList("References", message.references)
	}
	header.Set("Content-Type", "text/plain; charset=utf-8")
	header.Set("Content-Transfer-Encoding", "quoted-printable")
	var rendered bytes.Buffer
	writer, err := mail.CreateSingleInlineWriter(&rendered, header)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write([]byte(message.text)); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return rendered.Bytes(), nil
}

// sentMessage describes the rendered message in a Result.
func (message outgoingMessage) sentMessage() SentMessage {
	return SentMessage{
		MessageID: message.messageID, From: message.fromAddress, Recipients: message.envelopeRecipients(),
		Subject: message.subject, InReplyTo: message.inReplyTo, References: message.references, SentAt: message.date,
	}
}

// currentDateHeaderTime is now in UTC at the one-second precision of a Date header.
func currentDateHeaderTime() time.Time {
	return time.Now().UTC().Truncate(time.Second)
}

func buildAddressList(addresses []string) []*mail.Address {
	list := make([]*mail.Address, len(addresses))
	for index, address := range addresses {
		list[index] = &mail.Address{Address: address}
	}
	return list
}

// validateRecipientList requires bare ASCII addresses.
func validateRecipientList(field string, addresses []string) error {
	for index, address := range addresses {
		if !isBareEmailAddress(address) {
			return fmt.Errorf("%s[%d] must be one bare ASCII address such as jane@example.com", field, index)
		}
	}
	return nil
}

// validateSubject accepts one line of UTF-8 text of at most MaxSubjectBytes.
func validateSubject(field string, subject string) error {
	if len(subject) > MaxSubjectBytes || !utf8.ValidString(subject) || strings.ContainsFunc(subject, isControlCharacter) {
		return fmt.Errorf("%s must be one line of at most %d bytes", field, MaxSubjectBytes)
	}
	return nil
}

// validateBodyText accepts UTF-8 text with line breaks and tabs and no other control characters.
func validateBodyText(field string, text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(text) > MaxSendTextBytes || !utf8.ValidString(text) {
		return fmt.Errorf("%s must be UTF-8 text of at most %d bytes", field, MaxSendTextBytes)
	}
	if strings.ContainsFunc(text, func(character rune) bool {
		return isControlCharacter(character) && character != '\n' && character != '\r' && character != '\t'
	}) {
		return fmt.Errorf("%s cannot contain control characters other than line breaks and tabs", field)
	}
	return nil
}
