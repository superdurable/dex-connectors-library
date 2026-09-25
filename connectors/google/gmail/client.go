// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package gmail implements received-message Triggers plus message reads, sends, and replies.
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
	"net/url"
	"strconv"
	"strings"
	"time"

	connector "github.com/superdurable/dex-connectors-library/sdk/go"
)

type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	now        func() time.Time
}

func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

func withClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

type Client struct {
	endpoint         *url.URL
	httpClient       *http.Client
	credentials      connector.CredentialProvider[Credentials]
	maxResponseBytes int64
	maxMessageBytes  int64
	pollInterval     time.Duration
	pollPageSize     int
	now              func() time.Time
}

type SendMessageInput struct {
	To       []string `json:"to"`
	Subject  string   `json:"subject"`
	TextBody string   `json:"textBody"`
	HTMLBody string   `json:"htmlBody,omitempty"`
}

type SendMessageOutput struct {
	Sender     string   `json:"sender"`
	Recipients []string `json:"recipients"`
	MessageID  string   `json:"messageId"`
	ThreadID   string   `json:"threadId"`
}

type SendMessageOperation struct{ client *Client }

type sendResponse struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
}

func New(config Config, credentials connector.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Scheme == "" || endpoint.Hostname() == "" {
		return nil, fmt.Errorf("Gmail endpoint must be absolute")
	}
	if endpoint.Scheme != "https" && endpoint.Hostname() != "localhost" && endpoint.Hostname() != "127.0.0.1" {
		return nil, fmt.Errorf("Gmail endpoint must use HTTPS")
	}
	if credentials == nil {
		return nil, fmt.Errorf("credential provider is required")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Gmail connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.httpClient == nil {
		dependencies.httpClient = &http.Client{Timeout: 25 * time.Second}
	}
	if config.MaxResponseBytes < 1 || config.MaxMessageBytes < 1 || config.PollInterval < time.Second || config.PollPageSize < 1 || config.PollPageSize > 100 {
		return nil, fmt.Errorf("Gmail response limits and polling configuration are invalid")
	}
	return &Client{
		endpoint: endpoint, httpClient: dependencies.httpClient, credentials: credentials,
		maxResponseBytes: config.MaxResponseBytes, maxMessageBytes: config.MaxMessageBytes,
		pollInterval: config.PollInterval, pollPageSize: int(config.PollPageSize), now: dependencies.now,
	}, nil
}

func (client *Client) SendMessage() SendMessageOperation { return SendMessageOperation{client: client} }

func (SendMessageOperation) Definition() connector.MutationDefinition { return SendMessageDefinition }

func (SendMessageOperation) IdempotencyKey(callID connector.CallID, _ SendMessageInput) connector.IdempotencyKey {
	return connector.IdempotencyKey(callID)
}

func (operation SendMessageOperation) Invoke(call connector.Call, input SendMessageInput) connector.MutationAttempt[SendMessageOutput] {
	credential, err := operation.client.credentials.Resolve(call)
	if err != nil || credential.Validate() != nil {
		return connector.NewMutationBranch(SendMessageBranchRejected, SendMessageOutput{}, gmailFailurePointer(connector.FailureAuthentication, "connection credentials are unavailable"), connector.Receipt{})
	}
	recipients, validationFailure := validateSendInput(input, credential.PrimaryEmail)
	if validationFailure != nil {
		return connector.NewMutationBranch(SendMessageBranchDefect, SendMessageOutput{}, validationFailure, connector.Receipt{})
	}
	raw, err := buildMIMEMessage(call, credential.PrimaryEmail, recipients, input)
	if err != nil {
		return connector.NewMutationBranch(SendMessageBranchDefect, SendMessageOutput{}, gmailFailurePointer(connector.FailureValidation, "message could not be encoded"), connector.Receipt{})
	}
	if int64(len(raw)) > operation.client.maxMessageBytes {
		return connector.NewMutationBranch(SendMessageBranchDefect, SendMessageOutput{}, gmailFailurePointer(connector.FailureValidation, "message exceeds configured size limit"), connector.Receipt{})
	}
	payload, err := json.Marshal(map[string]string{"raw": base64.RawURLEncoding.EncodeToString(raw)})
	if err != nil {
		return connector.NewMutationBranch(SendMessageBranchDefect, SendMessageOutput{}, gmailFailurePointer(connector.FailureLocalDefect, "message request could not be encoded"), connector.Receipt{})
	}
	target := strings.TrimRight(operation.client.endpoint.String(), "/") + "/users/me/messages/send"
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return connector.NewMutationBranch(SendMessageBranchDefect, SendMessageOutput{}, gmailFailurePointer(connector.FailureLocalDefect, "message request could not be built"), connector.Receipt{})
	}
	request.Header.Set("Authorization", "Bearer "+credential.AccessToken.Reveal())
	request.Header.Set("Content-Type", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return connector.NewMutationUncertain(SendMessageOutput{}, gmailFailure(connector.FailureTransport, "Gmail send outcome is unknown"), operation.client.receipt(call, "", ""))
	}
	defer response.Body.Close()
	requestID := googleRequestID(response.Header)
	receipt := operation.client.receipt(call, requestID, "")
	content, readErr := io.ReadAll(io.LimitReader(response.Body, operation.client.maxResponseBytes+1))
	if readErr != nil || int64(len(content)) > operation.client.maxResponseBytes {
		return connector.NewMutationUncertain(SendMessageOutput{}, gmailFailure(connector.FailureTransport, "Gmail send response could not be confirmed"), receipt)
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return connector.NewMutationRetry[SendMessageOutput](gmailFailure(connector.FailureRateLimit, "Gmail temporarily rejected the send"), retryAfter(response.Header))
	}
	if response.StatusCode >= 500 {
		return connector.NewMutationUncertain(SendMessageOutput{}, gmailFailure(connector.FailureAvailability, "Gmail send outcome is unknown"), receipt)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return connector.NewMutationBranch(SendMessageBranchRejected, SendMessageOutput{}, gmailFailurePointer(statusFailureKind(response.StatusCode), "Gmail rejected the message"), receipt)
	}
	var sent sendResponse
	if err := json.Unmarshal(content, &sent); err != nil || sent.ID == "" {
		return connector.NewMutationUncertain(SendMessageOutput{}, gmailFailure(connector.FailureProtocol, "Gmail returned an invalid send response"), receipt)
	}
	receipt.ProviderObjectID = sent.ID
	return connector.NewMutationBranch(SendMessageBranchSent, SendMessageOutput{Sender: credential.PrimaryEmail, Recipients: recipients, MessageID: sent.ID, ThreadID: sent.ThreadID}, nil, receipt)
}

func validateSendInput(input SendMessageInput, sender string) ([]string, *connector.Failure) {
	if strings.TrimSpace(sender) == "" || strings.ContainsAny(input.Subject, "\r\n") || strings.TrimSpace(input.Subject) == "" || strings.TrimSpace(input.TextBody) == "" || len(input.To) == 0 {
		return nil, gmailFailurePointer(connector.FailureValidation, "sender, recipients, subject, and text body are required")
	}
	senderAddress, err := parseMailbox(sender)
	if err != nil || !strings.EqualFold(senderAddress.Address, strings.TrimSpace(sender)) {
		return nil, gmailFailurePointer(connector.FailureValidation, "authorized primary email is invalid")
	}
	recipients := make([]string, len(input.To))
	seen := map[string]bool{}
	for index, value := range input.To {
		address, err := parseMailbox(value)
		if err != nil {
			return nil, gmailFailurePointer(connector.FailureValidation, "recipient email is invalid")
		}
		canonical := strings.ToLower(address.Address)
		if seen[canonical] {
			return nil, gmailFailurePointer(connector.FailureValidation, "recipient email is duplicated")
		}
		seen[canonical] = true
		recipients[index] = address.String()
	}
	return recipients, nil
}

func parseMailbox(value string) (*mail.Address, error) {
	if strings.ContainsAny(value, "\r\n") {
		return nil, fmt.Errorf("mailbox contains a newline")
	}
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address == "" {
		return nil, fmt.Errorf("invalid mailbox")
	}
	return address, nil
}

func buildMIMEMessage(call connector.Call, sender string, recipients []string, input SendMessageInput) ([]byte, error) {
	var message bytes.Buffer
	writeHeader := func(name, value string) { fmt.Fprintf(&message, "%s: %s\r\n", name, value) }
	writeHeader("From", sender)
	writeHeader("To", strings.Join(recipients, ", "))
	writeHeader("Subject", mime.QEncoding.Encode("UTF-8", input.Subject))
	writeHeader("Message-ID", "<"+string(call.IdempotencyKey)+"@dex.superdurable.dev>")
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

func normalizeBody(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", "\r\n")
}

func retryAfter(header http.Header) time.Duration {
	seconds, _ := strconv.Atoi(header.Get("Retry-After"))
	if seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return 0
}

func statusFailureKind(status int) connector.FailureKind {
	switch status {
	case http.StatusUnauthorized:
		return connector.FailureAuthentication
	case http.StatusForbidden:
		return connector.FailureAuthorization
	case http.StatusNotFound:
		return connector.FailureNotFound
	case http.StatusConflict:
		return connector.FailureConflict
	default:
		return connector.FailureProviderRejection
	}
}

func gmailFailure(kind connector.FailureKind, message string) connector.Failure {
	return connector.Failure{Kind: kind, Provider: "gmail", Operation: "sendMessage", Message: message}
}

func gmailFailurePointer(kind connector.FailureKind, message string) *connector.Failure {
	failure := gmailFailure(kind, message)
	return &failure
}

func googleRequestID(header http.Header) string {
	if value := header.Get("X-Goog-Request-Id"); value != "" {
		return value
	}
	return header.Get("X-Request-Id")
}

func (client *Client) receipt(call connector.Call, requestID, objectID string) connector.Receipt {
	return connector.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "gmail", ProviderObjectID: objectID, ProviderRequestID: requestID, ObservedAt: client.now().UTC()}
}
