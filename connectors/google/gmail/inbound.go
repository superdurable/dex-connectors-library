// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/mail"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetMessageInput identifies one Gmail message.
type GetMessageInput struct {
	// MessageID is the provider message identifier.
	MessageID string `json:"messageId"`
}

// ReplyToMessageInput identifies a message and supplies the reply body.
type ReplyToMessageInput struct {
	// MessageID is the provider message identifier.
	MessageID string `json:"messageId"`
	// TextBody is the plain-text message body.
	TextBody string `json:"textBody"`
	// HTMLBody is the optional HTML message body.
	HTMLBody string `json:"htmlBody,omitempty"`
}

// Message is one decoded Gmail message.
type Message struct {
	// MessageID is the provider message identifier.
	MessageID string `json:"messageId"`
	// ThreadID is the provider conversation thread identifier.
	ThreadID string `json:"threadId"`
	// RFCMessageID is the decoded Message-ID header.
	RFCMessageID string `json:"rfcMessageId,omitempty"`
	// InReplyTo is the decoded In-Reply-To header.
	InReplyTo string `json:"inReplyTo,omitempty"`
	// References is the decoded References header.
	References string `json:"references,omitempty"`
	// From is the sender address.
	From string `json:"from"`
	// ReplyTo is the decoded Reply-To header.
	ReplyTo string `json:"replyTo,omitempty"`
	// To lists recipient addresses.
	To []string `json:"to,omitempty"`
	// Subject is the message subject.
	Subject string `json:"subject"`
	// TextBody is the plain-text message body.
	TextBody string `json:"textBody,omitempty"`
	// HTMLBody is the optional HTML message body.
	HTMLBody string `json:"htmlBody,omitempty"`
	// Snippet is the snippet for message.
	Snippet string `json:"snippet,omitempty"`
	// ReceivedAt is the received at for message.
	ReceivedAt time.Time `json:"receivedAt"`
	// LabelIDs lists the Gmail labels applied to the message.
	LabelIDs []string `json:"labelIds,omitempty"`
}

// GetMessageOperation reads one Gmail message.
type GetMessageOperation struct{ client *Client }

// ReplyToMessageOperation replies in an existing Gmail thread.
type ReplyToMessageOperation struct{ client *Client }

type gmailMessageResource struct {
	ID           string           `json:"id"`
	ThreadID     string           `json:"threadId"`
	LabelIDs     []string         `json:"labelIds"`
	Snippet      string           `json:"snippet"`
	InternalDate string           `json:"internalDate"`
	Payload      gmailMessagePart `json:"payload"`
}

type gmailMessagePart struct {
	MimeType string               `json:"mimeType"`
	Headers  []gmailMessageHeader `json:"headers"`
	Body     gmailMessageBody     `json:"body"`
	Parts    []gmailMessagePart   `json:"parts"`
}

type gmailMessageHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type gmailMessageBody struct {
	Data string `json:"data"`
}

type gmailHTTPResult struct {
	statusCode int
	header     http.Header
	body       []byte
}

// Definition returns the generated GetMessage operation definition.
func (GetMessageOperation) Definition() sdkgo.QueryDefinition { return GetMessageDefinition }

// Invoke reads and decodes one Gmail message.
func (operation GetMessageOperation) Invoke(call sdkgo.Call, input GetMessageInput) sdkgo.QueryAttempt[Message] {
	if strings.TrimSpace(input.MessageID) == "" {
		return sdkgo.NewQueryBranch(GetMessageBranchDefect, Message{}, gmailOperationFailurePointer("getMessage", sdkgo.FailureValidation, "message ID is required"), sdkgo.Receipt{})
	}
	credentials, err := operation.client.resolveCredentials(call.Context, call)
	if err != nil {
		return sdkgo.NewQueryBranch(GetMessageBranchDefect, Message{}, gmailOperationFailurePointer("getMessage", sdkgo.FailureAuthentication, "connection credentials are unavailable"), sdkgo.Receipt{})
	}
	resource, result, _, err := operation.client.readMessage(call.Context, call, credentials, input.MessageID, "full")
	if err != nil {
		return classifyGetMessageFailure(result, err)
	}
	message, err := decodeGmailMessage(resource)
	if err != nil {
		return sdkgo.NewQueryBranch(GetMessageBranchInvalidResponse, Message{}, gmailOperationFailurePointer("getMessage", sdkgo.FailureProtocol, "Gmail returned an invalid message"), operation.client.receipt(call, googleRequestID(result.header), input.MessageID))
	}
	return sdkgo.NewQueryBranch(GetMessageBranchRead, message, nil, sdkgo.Receipt{Provider: "gmail", ProviderObjectID: message.MessageID, ProviderRequestID: googleRequestID(result.header), ObservedAt: operation.client.now().UTC()})
}

// Definition returns the generated ReplyToMessage operation definition.
func (ReplyToMessageOperation) Definition() sdkgo.MutationDefinition {
	return ReplyToMessageDefinition
}

// IdempotencyKey derives the provider correlation key from the Dex Call ID.
func (ReplyToMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ ReplyToMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads the source message and sends one reply in its Gmail thread.
func (operation ReplyToMessageOperation) Invoke(call sdkgo.Call, input ReplyToMessageInput) sdkgo.MutationAttempt[SendMessageOutput] {
	credentials, err := operation.client.resolveCredentials(call.Context, call)
	if err != nil {
		return sdkgo.NewMutationBranch(ReplyToMessageBranchDefect, SendMessageOutput{}, gmailOperationFailurePointer("replyToMessage", sdkgo.FailureAuthentication, "connection credentials are unavailable"), sdkgo.Receipt{})
	}
	if strings.TrimSpace(input.MessageID) == "" || strings.TrimSpace(input.TextBody) == "" {
		return sdkgo.NewMutationBranch(ReplyToMessageBranchDefect, SendMessageOutput{}, gmailOperationFailurePointer("replyToMessage", sdkgo.FailureValidation, "message ID and text body are required"), sdkgo.Receipt{})
	}
	resource, result, credentials, err := operation.client.readMessage(call.Context, call, credentials, input.MessageID, "metadata")
	if err != nil {
		if result.statusCode == 0 || result.statusCode == http.StatusTooManyRequests || result.statusCode >= 500 {
			delay, retryAfterErr := retryAfter(result.header)
			if retryAfterErr != nil {
				return sdkgo.NewMutationRetry[SendMessageOutput](gmailOperationFailure("replyToMessage", sdkgo.FailureProtocol, "Gmail returned an invalid Retry-After header"), 0)
			}
			return sdkgo.NewMutationRetry[SendMessageOutput](gmailOperationFailure("replyToMessage", sdkgo.FailureAvailability, "Gmail source message is temporarily unavailable"), delay)
		}
		if result.statusCode >= 200 && result.statusCode < 300 {
			return sdkgo.NewMutationBranch(ReplyToMessageBranchInvalidResponse, SendMessageOutput{}, gmailOperationFailurePointer("replyToMessage", sdkgo.FailureProtocol, "Gmail returned an invalid source message response"), operation.client.receipt(call, googleRequestID(result.header), input.MessageID))
		}
		return sdkgo.NewMutationBranch(ReplyToMessageBranchProviderRejected, SendMessageOutput{}, gmailOperationFailurePointer("replyToMessage", statusFailureKind(result.statusCode), "Gmail rejected the source message lookup"), operation.client.receipt(call, googleRequestID(result.header), input.MessageID))
	}
	message, err := decodeGmailMessage(resource)
	if err != nil || message.ThreadID == "" || message.RFCMessageID == "" {
		return sdkgo.NewMutationBranch(ReplyToMessageBranchInvalidResponse, SendMessageOutput{}, gmailOperationFailurePointer("replyToMessage", sdkgo.FailureProtocol, "source message lacks reply metadata"), operation.client.receipt(call, googleRequestID(result.header), input.MessageID))
	}
	recipient, err := replyRecipient(message)
	if err != nil {
		return sdkgo.NewMutationBranch(ReplyToMessageBranchInvalidResponse, SendMessageOutput{}, gmailOperationFailurePointer("replyToMessage", sdkgo.FailureProtocol, "Gmail returned an invalid source sender"), operation.client.receipt(call, googleRequestID(result.header), input.MessageID))
	}
	if strings.ContainsAny(message.Subject+message.RFCMessageID+message.References, "\r\n") {
		return sdkgo.NewMutationBranch(ReplyToMessageBranchInvalidResponse, SendMessageOutput{}, gmailOperationFailurePointer("replyToMessage", sdkgo.FailureProtocol, "Gmail returned invalid source reply headers"), operation.client.receipt(call, googleRequestID(result.header), input.MessageID))
	}
	raw, err := buildReplyMIMEMessage(call, credentials.PrimaryEmail, recipient, message, input)
	if err != nil || int64(len(raw)) > operation.client.maxMessageBytes {
		return sdkgo.NewMutationBranch(ReplyToMessageBranchDefect, SendMessageOutput{}, gmailOperationFailurePointer("replyToMessage", sdkgo.FailureValidation, "reply could not be encoded within the configured limit"), sdkgo.Receipt{})
	}
	payload, err := json.Marshal(map[string]string{"raw": base64.RawURLEncoding.EncodeToString(raw), "threadId": message.ThreadID})
	if err != nil {
		return sdkgo.NewMutationBranch(ReplyToMessageBranchDefect, SendMessageOutput{}, gmailOperationFailurePointer("replyToMessage", sdkgo.FailureLocalDefect, "reply request could not be encoded"), sdkgo.Receipt{})
	}
	return operation.sendReply(call, credentials, recipient, message.ThreadID, payload)
}

func (operation ReplyToMessageOperation) sendReply(call sdkgo.Call, credentials Credentials, recipient string, threadID string, payload []byte) sdkgo.MutationAttempt[SendMessageOutput] {
	target := strings.TrimRight(operation.client.endpoint.String(), "/") + "/users/me/messages/send"
	response, credentials, err := operation.client.doAuthenticatedRequest(
		call.Context, call, credentials, http.MethodPost, target, payload, "application/json",
	)
	if err != nil {
		return sdkgo.NewMutationUncertain(SendMessageOutput{}, gmailOperationFailure("replyToMessage", sdkgo.FailureTransport, "Gmail reply outcome is unknown"), operation.client.receipt(call, "", ""))
	}
	defer response.Body.Close()
	receipt := operation.client.receipt(call, googleRequestID(response.Header), "")
	content, readErr := io.ReadAll(io.LimitReader(response.Body, operation.client.maxResponseBytes+1))
	if readErr != nil || int64(len(content)) > operation.client.maxResponseBytes || response.StatusCode >= 500 {
		return sdkgo.NewMutationUncertain(SendMessageOutput{}, gmailOperationFailure("replyToMessage", sdkgo.FailureAvailability, "Gmail reply outcome is unknown"), receipt)
	}
	if response.StatusCode == http.StatusTooManyRequests {
		delay, err := retryAfter(response.Header)
		if err != nil {
			return sdkgo.NewMutationRetry[SendMessageOutput](gmailOperationFailure("replyToMessage", sdkgo.FailureProtocol, "Gmail returned an invalid Retry-After header"), 0)
		}
		return sdkgo.NewMutationRetry[SendMessageOutput](gmailOperationFailure("replyToMessage", sdkgo.FailureRateLimit, "Gmail temporarily rejected the reply"), delay)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return sdkgo.NewMutationBranch(ReplyToMessageBranchProviderRejected, SendMessageOutput{}, gmailOperationFailurePointer("replyToMessage", statusFailureKind(response.StatusCode), "Gmail rejected the reply"), receipt)
	}
	var sent sendResponse
	if err := json.Unmarshal(content, &sent); err != nil || sent.ID == "" || (sent.ThreadID != "" && sent.ThreadID != threadID) {
		return sdkgo.NewMutationUncertain(SendMessageOutput{}, gmailOperationFailure("replyToMessage", sdkgo.FailureProtocol, "Gmail returned an invalid reply response"), receipt)
	}
	receipt.ProviderObjectID = sent.ID
	return sdkgo.NewMutationBranch(ReplyToMessageBranchSent, SendMessageOutput{Sender: credentials.PrimaryEmail, Recipients: []string{recipient}, MessageID: sent.ID, ThreadID: threadID}, nil, receipt)
}

func classifyGetMessageFailure(result gmailHTTPResult, err error) sdkgo.QueryAttempt[Message] {
	if result.statusCode == http.StatusTooManyRequests || result.statusCode >= 500 || result.statusCode == 0 {
		delay, retryAfterErr := retryAfter(result.header)
		if retryAfterErr != nil {
			return sdkgo.NewQueryRetry[Message](gmailOperationFailure("getMessage", sdkgo.FailureProtocol, "Gmail returned an invalid Retry-After header"), 0)
		}
		return sdkgo.NewQueryRetry[Message](gmailOperationFailure("getMessage", sdkgo.FailureAvailability, "Gmail message is temporarily unavailable"), delay)
	}
	if result.statusCode == http.StatusNotFound {
		return sdkgo.NewQueryBranch(GetMessageBranchNotFound, Message{}, nil, sdkgo.Receipt{})
	}
	if result.statusCode >= 200 && result.statusCode < 300 {
		return sdkgo.NewQueryBranch(GetMessageBranchInvalidResponse, Message{}, gmailOperationFailurePointer("getMessage", sdkgo.FailureProtocol, "Gmail returned an invalid or oversized message response"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(GetMessageBranchProviderRejected, Message{}, gmailOperationFailurePointer("getMessage", statusFailureKind(result.statusCode), err.Error()), sdkgo.Receipt{})
}

func decodeGmailMessage(resource gmailMessageResource) (Message, error) {
	if resource.ID == "" || resource.ThreadID == "" {
		return Message{}, fmt.Errorf("message identity is missing")
	}
	headers := gmailHeaders(resource.Payload.Headers)
	receivedMilliseconds, err := strconv.ParseInt(resource.InternalDate, 10, 64)
	if err != nil {
		return Message{}, fmt.Errorf("message date is invalid")
	}
	textBody, htmlBody, err := decodeGmailBodies(resource.Payload)
	if err != nil {
		return Message{}, err
	}
	return Message{
		MessageID: resource.ID, ThreadID: resource.ThreadID, RFCMessageID: headers["message-id"],
		InReplyTo: headers["in-reply-to"], References: headers["references"], From: headers["from"], ReplyTo: headers["reply-to"],
		To: splitAddressHeader(headers["to"]), Subject: decodeHeader(headers["subject"]), TextBody: textBody,
		HTMLBody: htmlBody, Snippet: resource.Snippet, ReceivedAt: time.UnixMilli(receivedMilliseconds).UTC(), LabelIDs: resource.LabelIDs,
	}, nil
}

func decodeGmailBodies(part gmailMessagePart) (string, string, error) {
	decoder := gmailBodyDecoder{}
	if err := decoder.visit(part); err != nil {
		return "", "", err
	}
	return strings.Join(decoder.textBodies, "\n"), strings.Join(decoder.htmlBodies, "\n"), nil
}

type gmailBodyDecoder struct {
	textBodies []string
	htmlBodies []string
}

func (decoder *gmailBodyDecoder) visit(part gmailMessagePart) error {
	if part.Body.Data != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(part.Body.Data)
		if err != nil {
			return err
		}
		switch strings.ToLower(part.MimeType) {
		case "text/plain":
			decoder.textBodies = append(decoder.textBodies, string(decoded))
		case "text/html":
			decoder.htmlBodies = append(decoder.htmlBodies, string(decoded))
		}
	}
	for _, child := range part.Parts {
		if err := decoder.visit(child); err != nil {
			return err
		}
	}
	return nil
}

func gmailHeaders(headers []gmailMessageHeader) map[string]string {
	result := make(map[string]string, len(headers))
	for _, header := range headers {
		result[strings.ToLower(header.Name)] = header.Value
	}
	return result
}

func splitAddressHeader(value string) []string {
	addresses, err := mail.ParseAddressList(value)
	if err != nil {
		return nil
	}
	result := make([]string, len(addresses))
	for index, address := range addresses {
		result[index] = address.String()
	}
	return result
}

func decodeHeader(value string) string {
	decoded, err := new(mime.WordDecoder).DecodeHeader(value)
	if err != nil {
		return value
	}
	return decoded
}

func replyRecipient(message Message) (string, error) {
	value := message.ReplyTo
	if value == "" {
		value = message.From
	}
	address, err := parseMailbox(value)
	if err != nil {
		return "", err
	}
	return address.String(), nil
}

func buildReplyMIMEMessage(call sdkgo.Call, sender string, recipient string, source Message, input ReplyToMessageInput) ([]byte, error) {
	senderAddress, err := parseMailbox(sender)
	if err != nil || !strings.EqualFold(senderAddress.Address, strings.TrimSpace(sender)) {
		return nil, fmt.Errorf("authorized primary email is invalid")
	}
	subject := source.Subject
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(subject)), "re:") {
		subject = "Re: " + subject
	}
	var message bytes.Buffer
	mustWriteMIMEHeader(&message, "From", sender)
	mustWriteMIMEHeader(&message, "To", recipient)
	mustWriteMIMEHeader(&message, "Subject", mime.QEncoding.Encode("UTF-8", subject))
	mustWriteMIMEHeader(&message, "Message-ID", "<"+string(call.IdempotencyKey)+"@dex.superdurable.dev>")
	mustWriteMIMEHeader(&message, "In-Reply-To", source.RFCMessageID)
	references := strings.TrimSpace(source.References + " " + source.RFCMessageID)
	mustWriteMIMEHeader(&message, "References", references)
	mustWriteMIMEHeader(&message, "X-Dex-Call-ID", string(call.ID))
	mustWriteMIMEHeader(&message, "MIME-Version", "1.0")
	if input.HTMLBody == "" {
		mustWriteMIMEHeader(&message, "Content-Type", `text/plain; charset="UTF-8"`)
		mustWriteMIMEHeader(&message, "Content-Transfer-Encoding", "8bit")
		message.WriteString("\r\n" + convertLineEndingsToCRLF(input.TextBody))
		return message.Bytes(), nil
	}
	hash := sha256.Sum256([]byte(call.IdempotencyKey))
	boundary := "dex-" + hex.EncodeToString(hash[:12])
	mustWriteMIMEHeader(&message, "Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	message.WriteString("\r\n")
	writer := multipart.NewWriter(&message)
	if err := writer.SetBoundary(boundary); err != nil {
		return nil, err
	}
	textHeader := make(textproto.MIMEHeader)
	textHeader.Set("Content-Type", `text/plain; charset="UTF-8"`)
	textPart, err := writer.CreatePart(textHeader)
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(textPart, convertLineEndingsToCRLF(input.TextBody)); err != nil {
		return nil, err
	}
	htmlHeader := make(textproto.MIMEHeader)
	htmlHeader.Set("Content-Type", `text/html; charset="UTF-8"`)
	htmlPart, err := writer.CreatePart(htmlHeader)
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(htmlPart, convertLineEndingsToCRLF(input.HTMLBody)); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return message.Bytes(), nil
}

func gmailOperationFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "gmail", Operation: operation, Message: message}
}

func gmailOperationFailurePointer(operation string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := gmailOperationFailure(operation, kind, message)
	return &failure
}
