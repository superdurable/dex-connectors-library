// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail

import (
	"bytes"
	"context"
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
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetMessageInput identifies one Gmail message.
type GetMessageInput struct {
	MessageID string `json:"messageId"`
}

// ReplyToMessageInput identifies a message and supplies the reply body.
type ReplyToMessageInput struct {
	MessageID string `json:"messageId"`
	TextBody  string `json:"textBody"`
	HTMLBody  string `json:"htmlBody,omitempty"`
}

// Message is one decoded Gmail message.
type Message struct {
	MessageID    string    `json:"messageId"`
	ThreadID     string    `json:"threadId"`
	RFCMessageID string    `json:"rfcMessageId,omitempty"`
	InReplyTo    string    `json:"inReplyTo,omitempty"`
	References   string    `json:"references,omitempty"`
	From         string    `json:"from"`
	ReplyTo      string    `json:"replyTo,omitempty"`
	To           []string  `json:"to,omitempty"`
	Subject      string    `json:"subject"`
	TextBody     string    `json:"textBody,omitempty"`
	HTMLBody     string    `json:"htmlBody,omitempty"`
	Snippet      string    `json:"snippet,omitempty"`
	ReceivedAt   time.Time `json:"receivedAt"`
	LabelIDs     []string  `json:"labelIds,omitempty"`
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

// GetMessage returns the query operation for one Gmail message.
func (client *Client) GetMessage() GetMessageOperation { return GetMessageOperation{client: client} }

// ReplyToMessage returns the mutation operation for one Gmail reply.
func (client *Client) ReplyToMessage() ReplyToMessageOperation {
	return ReplyToMessageOperation{client: client}
}

// Definition returns the generated GetMessage operation definition.
func (GetMessageOperation) Definition() sdkgo.QueryDefinition { return GetMessageDefinition }

// Invoke reads and decodes one Gmail message.
func (operation GetMessageOperation) Invoke(call sdkgo.Call, input GetMessageInput) sdkgo.QueryAttempt[Message] {
	if strings.TrimSpace(input.MessageID) == "" {
		return sdkgo.NewQueryBranch(GetMessageBranchDefect, Message{}, gmailOperationFailurePointer("getMessage", sdkgo.FailureValidation, "message ID is required"), sdkgo.Receipt{})
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return sdkgo.NewQueryBranch(GetMessageBranchDefect, Message{}, gmailOperationFailurePointer("getMessage", sdkgo.FailureAuthentication, "connection credentials are unavailable"), sdkgo.Receipt{})
	}
	resource, result, err := operation.client.readMessage(call.Context, credentials, input.MessageID, "full")
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
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return sdkgo.NewMutationBranch(ReplyToMessageBranchDefect, SendMessageOutput{}, gmailOperationFailurePointer("replyToMessage", sdkgo.FailureAuthentication, "connection credentials are unavailable"), sdkgo.Receipt{})
	}
	if strings.TrimSpace(input.MessageID) == "" || strings.TrimSpace(input.TextBody) == "" {
		return sdkgo.NewMutationBranch(ReplyToMessageBranchDefect, SendMessageOutput{}, gmailOperationFailurePointer("replyToMessage", sdkgo.FailureValidation, "message ID and text body are required"), sdkgo.Receipt{})
	}
	resource, result, err := operation.client.readMessage(call.Context, credentials, input.MessageID, "metadata")
	if err != nil {
		if result.statusCode == 0 || result.statusCode == http.StatusTooManyRequests || result.statusCode >= 500 {
			return sdkgo.NewMutationRetry[SendMessageOutput](gmailOperationFailure("replyToMessage", sdkgo.FailureAvailability, "Gmail source message is temporarily unavailable"), retryAfter(result.header))
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
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return sdkgo.NewMutationBranch(ReplyToMessageBranchDefect, SendMessageOutput{}, gmailOperationFailurePointer("replyToMessage", sdkgo.FailureLocalDefect, "reply request could not be built"), sdkgo.Receipt{})
	}
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	request.Header.Set("Content-Type", "application/json")
	response, err := operation.client.httpClient.Do(request)
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
		return sdkgo.NewMutationRetry[SendMessageOutput](gmailOperationFailure("replyToMessage", sdkgo.FailureRateLimit, "Gmail temporarily rejected the reply"), retryAfter(response.Header))
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

func (client *Client) readMessage(ctx context.Context, credentials Credentials, messageID string, format string) (gmailMessageResource, gmailHTTPResult, error) {
	query := url.Values{"format": {format}}
	if format == "metadata" {
		for _, name := range []string{"Message-ID", "In-Reply-To", "References", "From", "Reply-To", "To", "Subject"} {
			query.Add("metadataHeaders", name)
		}
	}
	target := strings.TrimRight(client.endpoint.String(), "/") + "/users/me/messages/" + url.PathEscape(messageID) + "?" + query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return gmailMessageResource{}, gmailHTTPResult{}, err
	}
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	response, err := client.httpClient.Do(request)
	if err != nil {
		return gmailMessageResource{}, gmailHTTPResult{}, err
	}
	defer response.Body.Close()
	result := gmailHTTPResult{statusCode: response.StatusCode, header: response.Header.Clone()}
	result.body, err = io.ReadAll(io.LimitReader(response.Body, client.maxResponseBytes+1))
	if err != nil || int64(len(result.body)) > client.maxResponseBytes || response.StatusCode < 200 || response.StatusCode >= 300 {
		return gmailMessageResource{}, result, fmt.Errorf("Gmail message lookup failed")
	}
	var resource gmailMessageResource
	if err := json.Unmarshal(result.body, &resource); err != nil {
		return gmailMessageResource{}, result, err
	}
	return resource, result, nil
}

func classifyGetMessageFailure(result gmailHTTPResult, err error) sdkgo.QueryAttempt[Message] {
	if result.statusCode == http.StatusTooManyRequests || result.statusCode >= 500 || result.statusCode == 0 {
		return sdkgo.NewQueryRetry[Message](gmailOperationFailure("getMessage", sdkgo.FailureAvailability, "Gmail message is temporarily unavailable"), retryAfter(result.header))
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
	var textBodies []string
	var htmlBodies []string
	var visit func(gmailMessagePart) error
	visit = func(current gmailMessagePart) error {
		if current.Body.Data != "" {
			decoded, err := base64.RawURLEncoding.DecodeString(current.Body.Data)
			if err != nil {
				return err
			}
			switch strings.ToLower(current.MimeType) {
			case "text/plain":
				textBodies = append(textBodies, string(decoded))
			case "text/html":
				htmlBodies = append(htmlBodies, string(decoded))
			}
		}
		for _, child := range current.Parts {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(part); err != nil {
		return "", "", err
	}
	return strings.Join(textBodies, "\n"), strings.Join(htmlBodies, "\n"), nil
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
	writeHeader := func(name string, value string) { fmt.Fprintf(&message, "%s: %s\r\n", name, value) }
	writeHeader("From", sender)
	writeHeader("To", recipient)
	writeHeader("Subject", mime.QEncoding.Encode("UTF-8", subject))
	writeHeader("Message-ID", "<"+string(call.IdempotencyKey)+"@dex.superdurable.dev>")
	writeHeader("In-Reply-To", source.RFCMessageID)
	references := strings.TrimSpace(source.References + " " + source.RFCMessageID)
	writeHeader("References", references)
	writeHeader("X-Dex-Call-ID", string(call.ID))
	writeHeader("MIME-Version", "1.0")
	if input.HTMLBody == "" {
		writeHeader("Content-Type", `text/plain; charset="UTF-8"`)
		writeHeader("Content-Transfer-Encoding", "8bit")
		message.WriteString("\r\n" + normalizeBody(input.TextBody))
		return message.Bytes(), nil
	}
	hash := sha256.Sum256([]byte(call.IdempotencyKey))
	boundary := "dex-" + hex.EncodeToString(hash[:12])
	writeHeader("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
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
	if _, err := io.WriteString(textPart, normalizeBody(input.TextBody)); err != nil {
		return nil, err
	}
	htmlHeader := make(textproto.MIMEHeader)
	htmlHeader.Set("Content-Type", `text/html; charset="UTF-8"`)
	htmlPart, err := writer.CreatePart(htmlHeader)
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(htmlPart, normalizeBody(input.HTMLBody)); err != nil {
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
