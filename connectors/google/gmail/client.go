// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package gmail implements received-message Triggers plus message reads, sends, and replies.
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
	"log/slog"
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

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	now        func() time.Time
	logger     *slog.Logger
}

// WithHTTPClient overrides the default HTTP client; the caller retains ownership.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithLogger sends the Trigger pollers' records to logger: failed polls, ignored and skipped messages,
// delivery retries, and the durable inboxes that NewProjectMessageTriggerRunner creates. Without this
// option, or with a nil logger, records go to slog.Default() as of each record. Records carry message
// and thread IDs and error messages, never senders, subjects, snippets, bodies, or tokens. The generated
// per-Trigger factories, such as NewProjectReplyReceivedTrigger, pass logger to their poller only; their
// durable inbox and runner records go to slog.Default(), so call slog.SetDefault when you use them.
func WithLogger(logger *slog.Logger) Option {
	return func(options *clientOptions) { options.logger = logger }
}

func withClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

// Client executes authenticated Gmail requests for connector operations.
type Client struct {
	endpoint         *url.URL
	httpClient       *http.Client
	credentials      CredentialSource
	refreshDriver    sdkgo.CredentialRefreshDriver[Credentials]
	maxResponseBytes int64
	maxMessageBytes  int64
	pollInterval     time.Duration
	pollPageSize     int
	now              func() time.Time
	logger           *slog.Logger
}

// SendMessageInput contains the provider request fields for send message.
type SendMessageInput struct {
	// To lists recipient addresses.
	To []string `json:"to"`
	// Subject is the message subject.
	Subject string `json:"subject"`
	// TextBody is the plain-text message body.
	TextBody string `json:"textBody"`
	// HTMLBody is the optional HTML message body.
	HTMLBody string `json:"htmlBody,omitempty"`
}

// SendMessageOutput contains the provider response fields for send message.
type SendMessageOutput struct {
	// Sender is the authenticated sender address.
	Sender string `json:"sender"`
	// Recipients lists the resolved recipient addresses.
	Recipients []string `json:"recipients"`
	// MessageID is the provider message identifier.
	MessageID string `json:"messageId"`
	// ThreadID is the provider conversation thread identifier.
	ThreadID string `json:"threadId"`
}

// SendMessageOperation implements the send message connector operation.
type SendMessageOperation struct{ client *Client }

type sendResponse struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
}

// New validates configuration and constructs an authenticated Gmail client.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
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
		refreshDriver:    NewCredentialRefreshDriver(dependencies.httpClient),
		maxResponseBytes: config.MaxResponseBytes, maxMessageBytes: config.MaxMessageBytes,
		pollInterval: config.PollInterval, pollPageSize: int(config.PollPageSize), now: dependencies.now,
		logger: dependencies.logger,
	}, nil
}

// SendMessage returns the SendMessage operation bound to this client.
func (client *Client) SendMessage() SendMessageOperation { return SendMessageOperation{client: client} }

// GetMessage returns the query operation for one Gmail message.
func (client *Client) GetMessage() GetMessageOperation { return GetMessageOperation{client: client} }

// ReplyToMessage returns the mutation operation for one Gmail reply.
func (client *Client) ReplyToMessage() ReplyToMessageOperation {
	return ReplyToMessageOperation{client: client}
}

func (client *Client) messageReceivedTriggerSource(connection sdkgo.ConnectionRef, configuration MessageReceivedTriggerConfiguration) sdkgo.TriggerSource[MessageEvent] {
	return client.messageReceivedPollingSource(connection, configuration)
}

func (client *Client) replyReceivedTriggerSource(connection sdkgo.ConnectionRef, configuration ReplyReceivedTriggerConfiguration) sdkgo.TriggerSource[MessageEvent] {
	return client.replyReceivedPollingSource(connection, configuration)
}

func (client *Client) messageReceivedPollingSource(connection sdkgo.ConnectionRef, configuration MessageReceivedTriggerConfiguration) *messagePollingTriggerSource {
	if err := configuration.Validate(); err != nil {
		panic(err)
	}
	return &messagePollingTriggerSource{
		client: client, connection: connection, searchQuery: configuration.SearchQuery,
		matcher: configuration.MessageMatcher, delivered: make(map[string]bool),
		triggerName: "messageReceived",
	}
}

func (client *Client) replyReceivedPollingSource(connection sdkgo.ConnectionRef, configuration ReplyReceivedTriggerConfiguration) *messagePollingTriggerSource {
	if err := configuration.Validate(); err != nil {
		panic(err)
	}
	return &messagePollingTriggerSource{
		client: client, connection: connection, searchQuery: configuration.SearchQuery,
		matcher: configuration.ReplyMatcher, requiresReply: true, delivered: make(map[string]bool),
		triggerName: "replyReceived",
	}
}

// triggerLogger returns the configured logger or, as of the call, slog.Default().
func (client *Client) triggerLogger() *slog.Logger {
	if client != nil && client.logger != nil {
		return client.logger
	}
	return slog.Default()
}

func (client *Client) resolveCredentials(ctx context.Context, call sdkgo.Call) (Credentials, error) {
	credentials, err := sdkgo.ResolveCredential(ctx, client.credentials, call, client.refreshDriver)
	if err != nil {
		return Credentials{}, err
	}
	if err := validateResolvedCredentials(credentials); err != nil {
		return Credentials{}, err
	}
	return credentials, nil
}

func (client *Client) doAuthenticatedRequest(
	ctx context.Context,
	call sdkgo.Call,
	credentials Credentials,
	method string,
	target string,
	payload []byte,
	contentType string,
) (*http.Response, Credentials, error) {
	for attempt := 0; attempt < 2; attempt++ {
		var body io.Reader
		if payload != nil {
			body = bytes.NewReader(payload)
		}
		request, err := http.NewRequestWithContext(ctx, method, target, body)
		if err != nil {
			return nil, Credentials{}, err
		}
		request.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}
		response, err := client.httpClient.Do(request)
		if err != nil {
			return nil, Credentials{}, err
		}
		if response.StatusCode != http.StatusUnauthorized || attempt != 0 {
			return response, credentials, nil
		}
		if _, ok := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !ok {
			return response, credentials, nil
		}
		// A 401 proves Gmail did not serve the request. When the one refresh is refused or fails, the
		// caller classifies the 401 itself instead of treating the request as possibly sent.
		refreshed, err := sdkgo.ResolveCredentialAfterRejection(ctx, client.credentials, call, client.refreshDriver)
		if err != nil || validateResolvedCredentials(refreshed) != nil {
			return response, credentials, nil
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, client.maxResponseBytes))
		_ = response.Body.Close()
		credentials = refreshed
	}
	return nil, Credentials{}, fmt.Errorf("Gmail authenticated request retry was exhausted")
}

func validateResolvedCredentials(credentials Credentials) error {
	if credentials.AccessToken.Reveal() == "" {
		return fmt.Errorf("Gmail access token is required")
	}
	address, err := mail.ParseAddress(credentials.PrimaryEmail)
	if err != nil || address.Address != strings.TrimSpace(credentials.PrimaryEmail) {
		return fmt.Errorf("Gmail primary email is invalid")
	}
	switch credentials.AuthMethodID {
	case "", GoogleOAuthAuthMethodID, WorkspaceDomainDelegationAuthMethodID:
		return nil
	default:
		return fmt.Errorf("Gmail authorization method is invalid")
	}
}

// Definition returns the immutable connector operation definition.
func (SendMessageOperation) Definition() sdkgo.MutationDefinition { return SendMessageDefinition }

// IdempotencyKey derives the provider key from the stable connector call ID.
func (SendMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ SendMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke executes one provider call and classifies its attempt.
func (operation SendMessageOperation) Invoke(call sdkgo.Call, input SendMessageInput) sdkgo.MutationAttempt[SendMessageOutput] {
	credential, err := operation.client.resolveCredentials(call.Context, call)
	if err != nil {
		return sdkgo.NewMutationBranch(SendMessageBranchDefect, SendMessageOutput{}, gmailFailurePointer(sdkgo.FailureAuthentication, "connection credentials are unavailable"), sdkgo.Receipt{})
	}
	recipients, validationFailure := validateSendInput(input, credential.PrimaryEmail)
	if validationFailure != nil {
		return sdkgo.NewMutationBranch(SendMessageBranchDefect, SendMessageOutput{}, validationFailure, sdkgo.Receipt{})
	}
	raw, err := buildMIMEMessage(call, credential.PrimaryEmail, recipients, input)
	if err != nil {
		return sdkgo.NewMutationBranch(SendMessageBranchDefect, SendMessageOutput{}, gmailFailurePointer(sdkgo.FailureValidation, "message could not be encoded"), sdkgo.Receipt{})
	}
	if int64(len(raw)) > operation.client.maxMessageBytes {
		return sdkgo.NewMutationBranch(SendMessageBranchDefect, SendMessageOutput{}, gmailFailurePointer(sdkgo.FailureValidation, "message exceeds configured size limit"), sdkgo.Receipt{})
	}
	payload, err := json.Marshal(map[string]string{"raw": base64.RawURLEncoding.EncodeToString(raw)})
	if err != nil {
		return sdkgo.NewMutationBranch(SendMessageBranchDefect, SendMessageOutput{}, gmailFailurePointer(sdkgo.FailureLocalDefect, "message request could not be encoded"), sdkgo.Receipt{})
	}
	target := strings.TrimRight(operation.client.endpoint.String(), "/") + "/users/me/messages/send"
	response, credential, err := operation.client.doAuthenticatedRequest(
		call.Context, call, credential, http.MethodPost, target, payload, "application/json",
	)
	if err != nil {
		return sdkgo.NewMutationUncertain(SendMessageOutput{}, gmailFailure(sdkgo.FailureTransport, "Gmail send outcome is unknown"), operation.client.receipt(call, "", ""))
	}
	defer response.Body.Close()
	requestID := googleRequestID(response.Header)
	receipt := operation.client.receipt(call, requestID, "")
	content, readErr := io.ReadAll(io.LimitReader(response.Body, operation.client.maxResponseBytes+1))
	if readErr != nil || int64(len(content)) > operation.client.maxResponseBytes {
		return sdkgo.NewMutationUncertain(SendMessageOutput{}, gmailFailure(sdkgo.FailureTransport, "Gmail send response could not be confirmed"), receipt)
	}
	if response.StatusCode == http.StatusTooManyRequests {
		delay, err := retryAfter(response.Header)
		if err != nil {
			return sdkgo.NewMutationRetry[SendMessageOutput](gmailFailure(sdkgo.FailureProtocol, "Gmail returned an invalid Retry-After header"), 0)
		}
		return sdkgo.NewMutationRetry[SendMessageOutput](gmailFailure(sdkgo.FailureRateLimit, "Gmail temporarily rejected the send"), delay)
	}
	if response.StatusCode >= 500 {
		return sdkgo.NewMutationUncertain(SendMessageOutput{}, gmailFailure(sdkgo.FailureAvailability, "Gmail send outcome is unknown"), receipt)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return sdkgo.NewMutationBranch(SendMessageBranchProviderRejected, SendMessageOutput{}, gmailFailurePointer(statusFailureKind(response.StatusCode), "Gmail rejected the message"), receipt)
	}
	var sent sendResponse
	if err := json.Unmarshal(content, &sent); err != nil || sent.ID == "" {
		return sdkgo.NewMutationUncertain(SendMessageOutput{}, gmailFailure(sdkgo.FailureProtocol, "Gmail returned an invalid send response"), receipt)
	}
	receipt.ProviderObjectID = sent.ID
	return sdkgo.NewMutationBranch(SendMessageBranchSent, SendMessageOutput{Sender: credential.PrimaryEmail, Recipients: recipients, MessageID: sent.ID, ThreadID: sent.ThreadID}, nil, receipt)
}

func validateSendInput(input SendMessageInput, sender string) ([]string, *sdkgo.Failure) {
	if strings.TrimSpace(sender) == "" || strings.ContainsAny(input.Subject, "\r\n") || strings.TrimSpace(input.Subject) == "" || strings.TrimSpace(input.TextBody) == "" || len(input.To) == 0 {
		return nil, gmailFailurePointer(sdkgo.FailureValidation, "sender, recipients, subject, and text body are required")
	}
	senderAddress, err := parseMailbox(sender)
	if err != nil || !strings.EqualFold(senderAddress.Address, strings.TrimSpace(sender)) {
		return nil, gmailFailurePointer(sdkgo.FailureValidation, "authorized primary email is invalid")
	}
	recipients := make([]string, len(input.To))
	seen := map[string]bool{}
	for index, value := range input.To {
		address, err := parseMailbox(value)
		if err != nil {
			return nil, gmailFailurePointer(sdkgo.FailureValidation, "recipient email is invalid")
		}
		canonical := strings.ToLower(address.Address)
		if seen[canonical] {
			return nil, gmailFailurePointer(sdkgo.FailureValidation, "recipient email is duplicated")
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

func buildMIMEMessage(call sdkgo.Call, sender string, recipients []string, input SendMessageInput) ([]byte, error) {
	var message bytes.Buffer
	mustWriteMIMEHeader(&message, "From", sender)
	mustWriteMIMEHeader(&message, "To", strings.Join(recipients, ", "))
	mustWriteMIMEHeader(&message, "Subject", mime.QEncoding.Encode("UTF-8", input.Subject))
	mustWriteMIMEHeader(&message, "Message-ID", "<"+string(call.IdempotencyKey)+"@dex.superdurable.dev>")
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

func mustWriteMIMEHeader(message *bytes.Buffer, name string, value string) {
	if _, err := fmt.Fprintf(message, "%s: %s\r\n", name, value); err != nil {
		panic(fmt.Sprintf("write MIME header: %v", err))
	}
}

func convertLineEndingsToCRLF(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", "\r\n")
}

func (client *Client) readMessage(
	ctx context.Context,
	call sdkgo.Call,
	credentials Credentials,
	messageID string,
	format string,
) (gmailMessageResource, gmailHTTPResult, Credentials, error) {
	query := url.Values{"format": {format}}
	if format == "metadata" {
		for _, name := range []string{"Message-ID", "In-Reply-To", "References", "From", "Reply-To", "To", "Subject"} {
			query.Add("metadataHeaders", name)
		}
	}
	target := strings.TrimRight(client.endpoint.String(), "/") + "/users/me/messages/" + url.PathEscape(messageID) + "?" + query.Encode()
	response, credentials, err := client.doAuthenticatedRequest(ctx, call, credentials, http.MethodGet, target, nil, "")
	if err != nil {
		return gmailMessageResource{}, gmailHTTPResult{}, Credentials{}, err
	}
	defer response.Body.Close()
	result := gmailHTTPResult{statusCode: response.StatusCode, header: response.Header.Clone()}
	result.body, err = io.ReadAll(io.LimitReader(response.Body, client.maxResponseBytes+1))
	if err != nil || int64(len(result.body)) > client.maxResponseBytes || response.StatusCode < 200 || response.StatusCode >= 300 {
		return gmailMessageResource{}, result, credentials, fmt.Errorf("Gmail message lookup failed")
	}
	var resource gmailMessageResource
	if err := json.Unmarshal(result.body, &resource); err != nil {
		return gmailMessageResource{}, result, credentials, err
	}
	return resource, result, credentials, nil
}

func retryAfter(header http.Header) (time.Duration, error) {
	value := header.Get("Retry-After")
	if value == "" {
		return 0, nil
	}
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse Retry-After %q: %w", value, err)
	}
	if seconds < 0 {
		return 0, fmt.Errorf("parse Retry-After %q: value cannot be negative", value)
	}
	if seconds > 0 {
		return time.Duration(seconds) * time.Second, nil
	}
	return 0, nil
}

func statusFailureKind(status int) sdkgo.FailureKind {
	switch status {
	case http.StatusUnauthorized:
		return sdkgo.FailureAuthentication
	case http.StatusForbidden:
		return sdkgo.FailureAuthorization
	case http.StatusNotFound:
		return sdkgo.FailureNotFound
	case http.StatusConflict:
		return sdkgo.FailureConflict
	default:
		return sdkgo.FailureProviderRejection
	}
}

func gmailFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "gmail", Operation: "sendMessage", Message: message}
}

func gmailFailurePointer(kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := gmailFailure(kind, message)
	return &failure
}

func googleRequestID(header http.Header) string {
	if value := header.Get("X-Goog-Request-Id"); value != "" {
		return value
	}
	return header.Get("X-Request-Id")
}

func (client *Client) receipt(call sdkgo.Call, requestID, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "gmail", ProviderObjectID: objectID, ProviderRequestID: requestID, ObservedAt: client.now().UTC()}
}
