// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email

import (
	"bufio"
	"bytes"
	"context"
	"slices"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	gomessage "github.com/emersion/go-message"
	"github.com/emersion/go-message/mail"
	"github.com/emersion/go-message/textproto"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	getMessageOperation = "getMessage"

	// maximumReferences bounds the References list read from a message and written to a reply.
	maximumReferences = 20
)

// GetMessageInput identifies the message to read.
type GetMessageInput struct {
	// Message is a reference from searchMessages; its UIDValidity must still match the mailbox.
	Message MessageReference `json:"message"`
}

// Message is one message's headers, bounded text, and attachment list, without attachment content.
type Message struct {
	MessageSummary
	// References lists at most 20 References message identifiers without angle brackets, oldest first.
	References []string `json:"references,omitempty"`
	// Text is the decoded body text, at most MaxTextBytes with line breaks written as \n.
	Text string `json:"text"`
	// TextSource names the part Text comes from.
	TextSource TextSource `json:"textSource"`
	// IsTextTruncated reports that Text was cut at MaxTextBytes or the server's body was longer than the connector reads.
	IsTextTruncated bool `json:"isTextTruncated,omitempty"`
	// Attachments lists at most MaxAttachments attachments in body order.
	Attachments []Attachment `json:"attachments,omitempty"`
	// HasMoreAttachments reports attachments beyond MaxAttachments.
	HasMoreAttachments bool `json:"hasMoreAttachments,omitempty"`
}

// GetMessageOperation is the getMessage Query.
type GetMessageOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (GetMessageOperation) Definition() sdkgo.QueryDefinition { return GetMessageDefinition }

// Invoke examines the mailbox read-only and fetches with BODY.PEEK, so reading never sets \Seen.
func (operation GetMessageOperation) Invoke(call sdkgo.Call, input GetMessageInput) sdkgo.QueryAttempt[Message] {
	if err := validateMessageReference("message", input.Message); err != nil {
		return sdkgo.NewQueryBranch(GetMessageBranchDefect, Message{},
			emailFailurePointer(sdkgo.FailureValidation, getMessageOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, getMessageOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetMessageBranchDefect, Message{}, failure, sdkgo.Receipt{})
	}
	ctx, cancel := context.WithTimeout(call.Context, imapOperationTimeout)
	defer cancel()
	session, sessionFailure := operation.client.openIMAPSession(ctx, credentials, getMessageOperation)
	if sessionFailure != nil {
		return getMessageAttemptForFailure(sessionFailure)
	}
	defer session.close()
	message, sessionFailure := readMessage(session, input.Message, true)
	if sessionFailure != nil {
		return getMessageAttemptForFailure(sessionFailure)
	}
	return sdkgo.NewQueryBranch(GetMessageBranchFound, message, nil, sdkgo.Receipt{ProviderObjectID: message.MessageID})
}

// readMessage selects the reference's mailbox read-only, checks UIDVALIDITY, and reads the message's headers
// and attachment list and, when shouldReadText is set, its text part.
func readMessage(session *imapSession, reference MessageReference, shouldReadText bool) (Message, *serverFailure) {
	if _, failure := session.selectMailbox(reference.Mailbox, true, reference.UIDValidity); failure != nil {
		return Message{}, failure
	}
	uid := imap.UID(reference.UID)
	options := summaryFetchOptions()
	options.BodySection = []*imap.FetchItemBodySection{referencesHeaderSection()}
	fetched, failure := session.fetchMessages([]imap.UID{uid}, options)
	if failure != nil {
		return Message{}, failure
	}
	headers := findFetchedMessage(fetched, uid)
	if headers == nil {
		return Message{}, &serverFailure{outcome: outcomeNotFound, failure: emailFailure(sdkgo.FailureNotFound, session.operation,
			"no message in the mailbox has the UID")}
	}
	summary, err := buildMessageSummary(reference.Mailbox, reference.UIDValidity, headers)
	if err != nil {
		return Message{}, &serverFailure{outcome: outcomeInvalidResponse, failure: emailFailure(sdkgo.FailureProtocol, session.operation, err.Error())}
	}
	referencesHeader := findBodySectionBytes(headers, func(section *imap.FetchItemBodySection) bool {
		return section.Specifier == imap.PartSpecifierHeader && len(section.Part) == 0
	})
	message := Message{MessageSummary: summary, References: parseReferences(referencesHeader), TextSource: TextSourceNone}
	if headers.BodyStructure == nil {
		return message, nil
	}
	attachments := findAttachments(headers.BodyStructure)
	if len(attachments) > MaxAttachments {
		attachments, message.HasMoreAttachments = attachments[:MaxAttachments], true
	}
	message.Attachments = attachments
	if !shouldReadText {
		return message, nil
	}
	parts := chooseTextPart(headers.BodyStructure)
	message.TextSource = parts.textSource
	if parts.textPart == nil {
		return message, nil
	}
	text, isTruncated, failure := readMessageText(session, uid, parts)
	if failure != nil {
		return Message{}, failure
	}
	message.Text, message.IsTextTruncated = text, isTruncated
	return message, nil
}

// readMessageText fetches at most maximumEncodedBodyFetchBytes of the chosen part with BODY.PEEK and decodes it.
func readMessageText(session *imapSession, uid imap.UID, parts messageBodyParts) (string, bool, *serverFailure) {
	section := &imap.FetchItemBodySection{Part: parts.textPath, Peek: true, Partial: &imap.SectionPartial{Offset: 0, Size: maximumEncodedBodyFetchBytes}}
	bodies, failure := session.fetchMessages([]imap.UID{uid}, &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{section}})
	if failure != nil {
		return "", false, failure
	}
	body := findFetchedMessage(bodies, uid)
	if body == nil {
		return "", false, &serverFailure{outcome: outcomeNotFound, failure: emailFailure(sdkgo.FailureNotFound, session.operation,
			"the message was removed while it was read")}
	}
	encoded := findBodySectionBytes(body, func(returned *imap.FetchItemBodySection) bool {
		return returned.Specifier == imap.PartSpecifierNone && slices.Equal(returned.Part, parts.textPath)
	})
	if len(encoded) > maximumEncodedBodyFetchBytes {
		encoded = encoded[:maximumEncodedBodyFetchBytes]
	}
	isPartial := int64(parts.textPart.Size) > maximumEncodedBodyFetchBytes
	text, isTruncated, err := decodeBodyText(parts.textPart, parts.textSource, encoded, isPartial)
	if err != nil {
		return "", false, &serverFailure{outcome: outcomeInvalidResponse, failure: emailFailure(sdkgo.FailureProtocol, session.operation,
			"the message's text part could not be decoded")}
	}
	return text, isTruncated, nil
}

// referencesHeaderSection fetches only the References header, without setting \Seen.
func referencesHeaderSection() *imap.FetchItemBodySection {
	return &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader, HeaderFields: []string{"References"}, Peek: true}
}

// parseReferences returns the last maximumReferences valid identifiers of a References header block.
func parseReferences(headerBlock []byte) []string {
	if len(headerBlock) == 0 {
		return nil
	}
	header, err := textproto.ReadHeader(bufio.NewReader(bytes.NewReader(headerBlock)))
	if err != nil {
		return nil
	}
	mailHeader := mail.Header{Header: gomessage.Header{Header: header}}
	// A malformed identifier ends the list; the identifiers parsed before it are kept.
	references, _ := mailHeader.MsgIDList("References")
	var valid []string
	for _, reference := range references {
		if isMessageIdentifier(reference) {
			valid = append(valid, reference)
		}
	}
	if len(valid) > maximumReferences {
		valid = valid[len(valid)-maximumReferences:]
	}
	return valid
}

// isMessageIdentifier accepts a msg-id body without angle brackets, spaces, or control characters.
func isMessageIdentifier(identifier string) bool {
	if identifier == "" || len(identifier) > 250 || !strings.Contains(identifier, "@") {
		return false
	}
	for index := 0; index < len(identifier); index++ {
		character := identifier[index]
		if character <= ' ' || character >= 0x7f || character == '<' || character == '>' {
			return false
		}
	}
	return true
}

// findBodySectionBytes matches a returned section by its own fields, because servers differ in whether they
// echo the requested partial range.
func findBodySectionBytes(message *imapclient.FetchMessageBuffer, matches func(*imap.FetchItemBodySection) bool) []byte {
	for _, section := range message.BodySection {
		if section.Section != nil && matches(section.Section) {
			return section.Bytes
		}
	}
	return nil
}

func findFetchedMessage(messages []*imapclient.FetchMessageBuffer, uid imap.UID) *imapclient.FetchMessageBuffer {
	for _, message := range messages {
		if message.UID == uid {
			return message
		}
	}
	return nil
}

func getMessageAttemptForFailure(failure *serverFailure) sdkgo.QueryAttempt[Message] {
	switch failure.outcome {
	case outcomeRetryable:
		return sdkgo.NewQueryRetry[Message](failure.failure, 0)
	case outcomeNotFound:
		return sdkgo.NewQueryBranch(GetMessageBranchNotFound, Message{}, &failure.failure, sdkgo.Receipt{})
	case outcomeInvalidResponse:
		return sdkgo.NewQueryBranch(GetMessageBranchInvalidResponse, Message{}, &failure.failure, sdkgo.Receipt{})
	default:
		return sdkgo.NewQueryBranch(GetMessageBranchProviderRejected, Message{}, &failure.failure, sdkgo.Receipt{})
	}
}
