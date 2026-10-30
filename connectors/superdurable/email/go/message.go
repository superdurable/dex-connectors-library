// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

const (
	// DefaultMailbox is the mailbox an operation uses when its input names none.
	DefaultMailbox = "INBOX"
	// MaxSubjectBytes bounds a returned subject; a longer one is cut on a UTF-8 boundary.
	MaxSubjectBytes = 998
	// MaxAddressesPerField bounds each returned address list, such as To or Cc.
	MaxAddressesPerField = 50

	maximumMailboxNameBytes = 255
	maximumDisplayNameBytes = 512
)

// MessageReference identifies one message in one IMAP mailbox. A UID names the same
// message only while the mailbox keeps the same UIDVALIDITY, so every operation that
// takes a reference checks UIDVALIDITY first and selects notFound when it changed.
type MessageReference struct {
	// Mailbox is the IMAP mailbox name, such as INBOX or Archive.
	Mailbox string `json:"mailbox"`
	// UIDValidity is the mailbox UIDVALIDITY value that UID belongs to; it is required.
	UIDValidity uint32 `json:"uidValidity"`
	// UID is the message's IMAP unique identifier in Mailbox; it is required.
	UID uint32 `json:"uid"`
}

// EmailAddress is one mailbox address with its optional display name.
type EmailAddress struct {
	// Name is the decoded display name, or empty.
	Name string `json:"name,omitempty"`
	// Address is the bare address, such as jane@example.com.
	Address string `json:"address"`
}

// MessageSummary is the envelope, flags, and size of one message, without its body.
type MessageSummary struct {
	// Reference identifies the message for getMessage, replyToMessage, moveMessage, and setFlags.
	Reference MessageReference `json:"reference"`
	// MessageID is the Message-ID header without angle brackets, or empty when the message has none.
	MessageID string `json:"messageId,omitempty"`
	// InReplyTo is the first In-Reply-To message identifier without angle brackets, or empty.
	InReplyTo string `json:"inReplyTo,omitempty"`
	// From is the first From address; its Address is empty when the header is missing or malformed.
	From EmailAddress `json:"from"`
	// ReplyTo holds the Reply-To addresses, where a reply goes. IMAP servers report From here when the
	// message has no Reply-To header, as the IMAP ENVELOPE definition requires.
	ReplyTo []EmailAddress `json:"replyTo,omitempty"`
	// To holds at most MaxAddressesPerField To addresses.
	To []EmailAddress `json:"to,omitempty"`
	// Cc holds at most MaxAddressesPerField Cc addresses.
	Cc []EmailAddress `json:"cc,omitempty"`
	// Subject is the decoded subject, cut at MaxSubjectBytes.
	Subject string `json:"subject"`
	// SentAt is the Date header, or zero when it is missing or unparseable.
	SentAt time.Time `json:"sentAt,omitempty"`
	// ReceivedAt is the server's internal date, when the message arrived in this mailbox.
	ReceivedAt time.Time `json:"receivedAt"`
	// IsSeen reports the \Seen flag.
	IsSeen bool `json:"isSeen"`
	// IsFlagged reports the \Flagged flag.
	IsFlagged bool `json:"isFlagged"`
	// IsAnswered reports the \Answered flag.
	IsAnswered bool `json:"isAnswered"`
	// SizeBytes is the RFC 822 size of the whole message in bytes.
	SizeBytes int64 `json:"sizeBytes"`
	// HasAttachments reports a part with an attachment disposition or a file name.
	HasAttachments bool `json:"hasAttachments"`
}

// validateMessageReference checks a reference before any connection is opened.
func validateMessageReference(field string, reference MessageReference) error {
	if err := validateMailboxName(field+".mailbox", reference.Mailbox); err != nil {
		return err
	}
	if reference.UIDValidity == 0 {
		return fmt.Errorf("%s.uidValidity is required; copy it from the searchMessages result", field)
	}
	if reference.UID == 0 {
		return fmt.Errorf("%s.uid is required; copy it from the searchMessages result", field)
	}
	return nil
}

// validateMailboxName accepts a UTF-8 mailbox name without control characters; the IMAP client encodes it.
func validateMailboxName(field string, name string) error {
	if name == "" {
		return fmt.Errorf("%s is required, such as INBOX", field)
	}
	if len(name) > maximumMailboxNameBytes || !utf8.ValidString(name) || strings.ContainsFunc(name, isControlCharacter) ||
		strings.TrimSpace(name) != name || strings.ContainsAny(name, "*%") {
		return fmt.Errorf("%s must be a mailbox name of at most %d bytes without control characters, wildcards, or surrounding spaces", field, maximumMailboxNameBytes)
	}
	return nil
}

// mailboxOrDefault returns DefaultMailbox for a blank name.
func mailboxOrDefault(name string) string {
	if name == "" {
		return DefaultMailbox
	}
	return name
}

// isSameMailbox compares names; INBOX is case-insensitive in IMAP and every other name is case-sensitive.
func isSameMailbox(first string, second string) bool {
	if strings.EqualFold(first, DefaultMailbox) && strings.EqualFold(second, DefaultMailbox) {
		return true
	}
	return first == second
}

// buildMessageSummary converts one FETCH response, which must carry UID, FLAGS, ENVELOPE, INTERNALDATE, and RFC822.SIZE.
func buildMessageSummary(mailbox string, uidValidity uint32, fetched *imapclient.FetchMessageBuffer) (MessageSummary, error) {
	if fetched.UID == 0 || fetched.Envelope == nil {
		return MessageSummary{}, errors.New("the IMAP server's FETCH response has no UID or ENVELOPE")
	}
	envelope := fetched.Envelope
	summary := MessageSummary{
		Reference: MessageReference{Mailbox: mailbox, UIDValidity: uidValidity, UID: uint32(fetched.UID)},
		MessageID: envelope.MessageID, Subject: truncateUTF8(envelope.Subject, MaxSubjectBytes),
		ReceivedAt: fetched.InternalDate.UTC(), SizeBytes: fetched.RFC822Size,
		ReplyTo: convertAddressList(envelope.ReplyTo), To: convertAddressList(envelope.To), Cc: convertAddressList(envelope.Cc),
		HasAttachments: fetched.BodyStructure != nil && len(findAttachments(fetched.BodyStructure)) > 0,
	}
	if !envelope.Date.IsZero() {
		summary.SentAt = envelope.Date.UTC()
	}
	if len(envelope.InReplyTo) > 0 {
		summary.InReplyTo = envelope.InReplyTo[0]
	}
	if from := convertAddressList(envelope.From); len(from) > 0 {
		summary.From = from[0]
	}
	for _, flag := range fetched.Flags {
		switch {
		case strings.EqualFold(string(flag), string(imap.FlagSeen)):
			summary.IsSeen = true
		case strings.EqualFold(string(flag), string(imap.FlagFlagged)):
			summary.IsFlagged = true
		case strings.EqualFold(string(flag), string(imap.FlagAnswered)):
			summary.IsAnswered = true
		}
	}
	return summary, nil
}

// convertAddressList keeps real addresses, skips group markers, and stops at MaxAddressesPerField.
func convertAddressList(addresses []imap.Address) []EmailAddress {
	var converted []EmailAddress
	for _, address := range addresses {
		bare := address.Addr()
		if bare == "" {
			continue
		}
		converted = append(converted, EmailAddress{Name: truncateUTF8(address.Name, maximumDisplayNameBytes), Address: bare})
		if len(converted) == MaxAddressesPerField {
			break
		}
	}
	return converted
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
