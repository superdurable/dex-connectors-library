// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// DefaultFolder is the well-known Inbox folder that searchMessages uses when the input leaves folder blank.
	DefaultFolder = "inbox"
	// MaxListedAddresses bounds each To, Cc, Bcc, and Reply-To list in a returned summary or message.
	MaxListedAddresses = 50
	// MaxListedCategories bounds the categories returned for one message and accepted by setMessageFlags.
	MaxListedCategories = 25
	// MaxCategoryCharacters bounds one category name, the length Outlook accepts for a category.
	MaxCategoryCharacters = 255
	// MaxSubjectCharacters is Exchange Online's subject length limit.
	MaxSubjectCharacters = 255
	// MaxPreviewCharacters bounds the body preview in a summary; Graph's own preview is at most 255 characters.
	MaxPreviewCharacters = 255

	// maximumGraphIDBytes bounds a message or folder ID; Graph IDs are about 150 characters.
	maximumGraphIDBytes = 1024
)

var (
	// graphIDPattern is the URL-safe base64 alphabet of Graph REST IDs, so an ID never adds a path segment.
	graphIDPattern = regexp.MustCompile(`^[A-Za-z0-9=+_-]+$`)
	// wellKnownFolderNames are Graph's well-known mail folder names, which every mailbox resolves.
	wellKnownFolderNames = map[string]bool{
		"archive": true, "clutter": true, "conflicts": true, "conversationhistory": true, "deleteditems": true,
		"drafts": true, "inbox": true, "junkemail": true, "localfailures": true, "msgfolderroot": true,
		"outbox": true, "recoverableitemsdeletions": true, "scheduled": true, "searchfolders": true,
		"sentitems": true, "serverfailures": true, "syncissues": true,
	}
)

// EmailAddress is one mailbox address with its optional display name.
type EmailAddress struct {
	// Name is the display name, such as Jane Smith, or empty.
	Name string `json:"name,omitempty"`
	// Address is the SMTP address, such as jane@acme.example.com.
	Address string `json:"address"`
}

// FlagStatus is a message's follow-up flag, using Graph's own values.
type FlagStatus string

const (
	// FlagStatusNotFlagged means the message has no follow-up flag.
	FlagStatusNotFlagged FlagStatus = "notFlagged"
	// FlagStatusFlagged means the message is flagged for follow-up.
	FlagStatusFlagged FlagStatus = "flagged"
	// FlagStatusComplete means the follow-up was marked complete.
	FlagStatusComplete FlagStatus = "complete"
)

// AttachmentKind is the Graph attachment type, without the #microsoft.graph prefix.
type AttachmentKind string

const (
	// AttachmentKindFile is a file attached to the message.
	AttachmentKindFile AttachmentKind = "file"
	// AttachmentKindItem is an attached Outlook item, such as a forwarded message or an event.
	AttachmentKindItem AttachmentKind = "item"
	// AttachmentKindReference is a link to a file in OneDrive or SharePoint.
	AttachmentKindReference AttachmentKind = "reference"
)

// MessageSummary is one message's header fields and state, without its body.
type MessageSummary struct {
	// ID is the message's immutable Graph ID. It stays the same when the message moves to another folder
	// of the mailbox, so later operations can use it after moveMessage.
	ID string `json:"id"`
	// ConversationID groups the messages of one thread.
	ConversationID string `json:"conversationId,omitempty"`
	// InternetMessageID is the Message-ID header, with its angle brackets as Graph returns them.
	InternetMessageID string `json:"internetMessageId,omitempty"`
	// FolderID is the ID of the mail folder that holds the message.
	FolderID string `json:"folderId,omitempty"`
	// Subject is the subject line, cut at MaxSubjectCharacters.
	Subject string `json:"subject"`
	// From is the author shown to recipients; it can differ from Sender for a delegated send.
	From *EmailAddress `json:"from,omitempty"`
	// Sender is the mailbox that actually sent the message.
	Sender *EmailAddress `json:"sender,omitempty"`
	// ReplyTo lists the Reply-To addresses; empty means replies go to From.
	ReplyTo []EmailAddress `json:"replyTo,omitempty"`
	// To lists at most MaxListedAddresses primary recipients.
	To []EmailAddress `json:"to,omitempty"`
	// Cc lists at most MaxListedAddresses copied recipients.
	Cc []EmailAddress `json:"cc,omitempty"`
	// ReceivedAt is when the mailbox received the message, in UTC.
	ReceivedAt time.Time `json:"receivedAt"`
	// SentAt is when the message was sent, in UTC, or nil for a draft.
	SentAt *time.Time `json:"sentAt,omitempty"`
	// IsRead reports whether the message is marked read.
	IsRead bool `json:"isRead"`
	// IsDraft reports an unsent draft.
	IsDraft bool `json:"isDraft,omitempty"`
	// FlagStatus is the follow-up flag.
	FlagStatus FlagStatus `json:"flagStatus"`
	// Categories lists at most MaxListedCategories category names.
	Categories []string `json:"categories,omitempty"`
	// Importance is low, normal, or high.
	Importance string `json:"importance,omitempty"`
	// HasAttachments reports attachments other than inline images, as Graph defines it.
	HasAttachments bool `json:"hasAttachments"`
	// Preview is the first part of the body as plain text, at most MaxPreviewCharacters.
	Preview string `json:"preview,omitempty"`
}

// Message is one message with its bounded plain-text body and attachment list.
type Message struct {
	MessageSummary
	// Bcc lists blind-copied recipients, which Graph returns only for messages this mailbox sent.
	Bcc []EmailAddress `json:"bcc,omitempty"`
	// Text is the body as plain text, cut at MaxTextBytes on a UTF-8 boundary.
	Text string `json:"text"`
	// IsTextTruncated reports that Text was cut.
	IsTextTruncated bool `json:"isTextTruncated,omitempty"`
	// TextSource is text when Graph converted the body to plain text, or html when the connector removed
	// HTML markup itself because Graph returned HTML.
	TextSource string `json:"textSource"`
	// WebLink opens the message in Outlook on the web for a person reviewing it.
	WebLink string `json:"webLink,omitempty"`
	// Attachments lists at most MaxListedAttachments attachments without their content.
	Attachments []Attachment `json:"attachments,omitempty"`
	// IsAttachmentListTruncated reports that the message has more attachments than were listed.
	IsAttachmentListTruncated bool `json:"isAttachmentListTruncated,omitempty"`
}

// Attachment describes one attachment without its content.
type Attachment struct {
	// ID is the attachment's Graph ID.
	ID string `json:"id"`
	// Name is the file or item name.
	Name string `json:"name"`
	// ContentType is the media type, such as application/pdf, when Graph reports one.
	ContentType string `json:"contentType,omitempty"`
	// SizeBytes is the attachment size Graph reports, in bytes.
	SizeBytes int64 `json:"sizeBytes"`
	// IsInline reports an attachment shown in the body, such as an embedded image.
	IsInline bool `json:"isInline,omitempty"`
	// Kind is file, item, or reference.
	Kind AttachmentKind `json:"kind"`
}

// graphEmailAddressWire is Graph's recipient object.
type graphEmailAddressWire struct {
	EmailAddress struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	} `json:"emailAddress"`
}

// graphFlagWire is Graph's followupFlag object.
type graphFlagWire struct {
	FlagStatus string `json:"flagStatus"`
}

// graphMessageWire holds the message properties the connector selects.
type graphMessageWire struct {
	ID                string                  `json:"id"`
	ConversationID    string                  `json:"conversationId"`
	InternetMessageID string                  `json:"internetMessageId"`
	ParentFolderID    string                  `json:"parentFolderId"`
	Subject           string                  `json:"subject"`
	From              *graphEmailAddressWire  `json:"from"`
	Sender            *graphEmailAddressWire  `json:"sender"`
	ReplyTo           []graphEmailAddressWire `json:"replyTo"`
	ToRecipients      []graphEmailAddressWire `json:"toRecipients"`
	CcRecipients      []graphEmailAddressWire `json:"ccRecipients"`
	BccRecipients     []graphEmailAddressWire `json:"bccRecipients"`
	ReceivedDateTime  *time.Time              `json:"receivedDateTime"`
	SentDateTime      *time.Time              `json:"sentDateTime"`
	CreatedDateTime   *time.Time              `json:"createdDateTime"`
	IsRead            bool                    `json:"isRead"`
	IsDraft           bool                    `json:"isDraft"`
	Flag              *graphFlagWire          `json:"flag"`
	Categories        []string                `json:"categories"`
	Importance        string                  `json:"importance"`
	HasAttachments    bool                    `json:"hasAttachments"`
	BodyPreview       string                  `json:"bodyPreview"`
	WebLink           string                  `json:"webLink"`
	Body              *struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
}

// summaryProperties is the $select list for a MessageSummary.
var summaryProperties = []string{
	"id", "conversationId", "internetMessageId", "parentFolderId", "subject", "from", "sender", "replyTo",
	"toRecipients", "ccRecipients", "receivedDateTime", "sentDateTime", "isRead", "isDraft", "flag",
	"categories", "importance", "hasAttachments", "bodyPreview",
}

// decodeGraphMessage decodes one message object and checks the fields every operation relies on.
func decodeGraphMessage(body []byte) (graphMessageWire, error) {
	var message graphMessageWire
	if err := json.Unmarshal(body, &message); err != nil {
		return graphMessageWire{}, errors.New("response is not a message object")
	}
	if err := validateGraphID("message id", message.ID); err != nil {
		return graphMessageWire{}, errors.New("response has no usable message id")
	}
	return message, nil
}

// summarize converts a decoded message into a bounded MessageSummary.
func (message graphMessageWire) summarize() MessageSummary {
	summary := MessageSummary{
		ID: message.ID, ConversationID: message.ConversationID, InternetMessageID: message.InternetMessageID,
		FolderID: message.ParentFolderID, Subject: truncateCharacters(message.Subject, MaxSubjectCharacters),
		From: convertOptionalAddress(message.From), Sender: convertOptionalAddress(message.Sender),
		ReplyTo: convertAddressList(message.ReplyTo), To: convertAddressList(message.ToRecipients),
		Cc: convertAddressList(message.CcRecipients), IsRead: message.IsRead, IsDraft: message.IsDraft,
		FlagStatus: FlagStatusNotFlagged, Importance: message.Importance, HasAttachments: message.HasAttachments,
		Preview: truncateCharacters(message.BodyPreview, MaxPreviewCharacters),
	}
	if message.ReceivedDateTime != nil {
		summary.ReceivedAt = message.ReceivedDateTime.UTC()
	}
	if message.SentDateTime != nil && !message.IsDraft {
		sentAt := message.SentDateTime.UTC()
		summary.SentAt = &sentAt
	}
	if message.Flag != nil && message.Flag.FlagStatus != "" {
		summary.FlagStatus = FlagStatus(message.Flag.FlagStatus)
	}
	if len(message.Categories) > 0 {
		summary.Categories = append([]string(nil), message.Categories[:min(len(message.Categories), MaxListedCategories)]...)
	}
	return summary
}

func convertOptionalAddress(wire *graphEmailAddressWire) *EmailAddress {
	if wire == nil || wire.EmailAddress.Address == "" {
		return nil
	}
	return &EmailAddress{Name: wire.EmailAddress.Name, Address: wire.EmailAddress.Address}
}

func convertAddressList(wires []graphEmailAddressWire) []EmailAddress {
	var addresses []EmailAddress
	for _, wire := range wires {
		if len(addresses) == MaxListedAddresses {
			break
		}
		if wire.EmailAddress.Address != "" {
			addresses = append(addresses, EmailAddress{Name: wire.EmailAddress.Name, Address: wire.EmailAddress.Address})
		}
	}
	return addresses
}

// recipientAddresses lists every To, Cc, and Bcc address of a draft or sent message, in that order.
func (message graphMessageWire) recipientAddresses() []string {
	var addresses []string
	for _, list := range [][]graphEmailAddressWire{message.ToRecipients, message.CcRecipients, message.BccRecipients} {
		for _, recipient := range list {
			if recipient.EmailAddress.Address != "" {
				addresses = append(addresses, recipient.EmailAddress.Address)
			}
		}
	}
	return addresses
}

// buildGraphRecipients converts validated bare addresses into Graph recipient objects.
func buildGraphRecipients(addresses []string) []graphEmailAddressWire {
	recipients := make([]graphEmailAddressWire, 0, len(addresses))
	for _, address := range addresses {
		var recipient graphEmailAddressWire
		recipient.EmailAddress.Address = address
		recipients = append(recipients, recipient)
	}
	return recipients
}

// validateGraphID checks an opaque Graph message or folder ID before it enters a URL path.
func validateGraphID(fieldName string, value string) error {
	if value == "" || len(value) > maximumGraphIDBytes || !graphIDPattern.MatchString(value) {
		return fmt.Errorf("%s must be a Microsoft Graph ID from an earlier Result, such as the id of searchMessages", fieldName)
	}
	return nil
}

// resolveFolderSegment returns a well-known folder name in lowercase or a validated folder ID.
func resolveFolderSegment(fieldName string, folder string, fallback string) (string, error) {
	trimmed := strings.TrimSpace(folder)
	if trimmed == "" {
		trimmed = fallback
	}
	if trimmed == "" {
		return "", fmt.Errorf("%s is required: a folder ID from the mail folder picker or a well-known name such as archive", fieldName)
	}
	if wellKnownFolderNames[strings.ToLower(trimmed)] {
		return strings.ToLower(trimmed), nil
	}
	if err := validateGraphID(fieldName, trimmed); err != nil {
		return "", fmt.Errorf("%s must be a folder ID from the mail folder picker or a well-known name such as inbox, archive, or deleteditems", fieldName)
	}
	return trimmed, nil
}

// validateBareAddresses checks bare SMTP addresses such as jane@acme.example.com.
func validateBareAddresses(fieldName string, addresses []string) error {
	for _, address := range addresses {
		parsed, err := mail.ParseAddress(address)
		if err != nil || parsed.Name != "" || parsed.Address != address || strings.ContainsAny(address, "\r\n") {
			return fmt.Errorf("%s must contain bare addresses such as jane@acme.example.com", fieldName)
		}
	}
	return nil
}

// validateSingleLine checks one line of valid UTF-8 text with a character limit.
func validateSingleLine(fieldName string, value string, maximumCharacters int) error {
	switch {
	case !utf8.ValidString(value):
		return fmt.Errorf("%s must be valid UTF-8", fieldName)
	case utf8.RuneCountInString(value) > maximumCharacters:
		return fmt.Errorf("%s can be at most %d characters", fieldName, maximumCharacters)
	}
	for _, character := range value {
		if character < ' ' || character == 0x7f {
			return fmt.Errorf("%s must be one line without control characters", fieldName)
		}
	}
	return nil
}

// truncateCharacters cuts value to at most maximumCharacters runes.
func truncateCharacters(value string, maximumCharacters int) string {
	if utf8.RuneCountInString(value) <= maximumCharacters {
		return value
	}
	return string([]rune(value)[:maximumCharacters])
}

// truncateUTF8Bytes cuts value to at most maximumBytes bytes on a rune boundary.
func truncateUTF8Bytes(value string, maximumBytes int) (string, bool) {
	if len(value) <= maximumBytes {
		return value, false
	}
	cut := maximumBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut], true
}
