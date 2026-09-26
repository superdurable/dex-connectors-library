// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// MessageMatcher filters received Gmail messages by sender and text.
type MessageMatcher struct {
	MessageContains string   `json:"messageContains,omitempty"`
	SenderEmails    []string `json:"senderEmails,omitempty"`
}

// MessageReceivedTriggerConfiguration configures root-message polling.
type MessageReceivedTriggerConfiguration struct {
	SearchQuery    string         `json:"searchQuery,omitempty"`
	MessageMatcher MessageMatcher `json:"messageMatcher,omitempty"`
}

// ReplyReceivedTriggerConfiguration configures reply polling.
type ReplyReceivedTriggerConfiguration struct {
	SearchQuery  string         `json:"searchQuery,omitempty"`
	ReplyMatcher MessageMatcher `json:"replyMatcher,omitempty"`
}

// MessageEvent is the stable, metadata-only payload emitted by Gmail Triggers.
type MessageEvent struct {
	PrimaryEmail string    `json:"primaryEmail"`
	MessageID    string    `json:"messageId"`
	ThreadID     string    `json:"threadId"`
	From         string    `json:"from"`
	Subject      string    `json:"subject"`
	Snippet      string    `json:"snippet,omitempty"`
	ReceivedAt   time.Time `json:"receivedAt"`
	IsReply      bool      `json:"isReply"`
}

type messagePollingTriggerSource struct {
	client        *Client
	connection    sdkgo.ConnectionRef
	searchQuery   string
	matcher       MessageMatcher
	requiresReply bool
	delivered     map[string]bool
	// retry holds the event whose delivery failed on an earlier poll. Every later poll delivers it before
	// any listed message, whether or not Gmail still lists it, until the target consumes it. Delivery stops
	// at the first failure, so at most one event per source waits for a retry.
	retry *pendingMessageRetry
	// pollFailures counts the consecutive polls that could not list or read Gmail.
	pollFailures int
	// triggerName and bindingName identify the source in log records. A source built by a generated
	// per-Trigger factory has no binding name.
	triggerName string
	bindingName string
}

// pendingMessageRetry is an event that a target failed to consume, with the number of failed attempts.
type pendingMessageRetry struct {
	event    sdkgo.TriggerEvent[MessageEvent]
	failures int
}

type gmailMessageList struct {
	Messages []struct {
		ID       string `json:"id"`
		ThreadID string `json:"threadId"`
	} `json:"messages"`
}

// Validate checks root-message Trigger configuration.
func (configuration MessageReceivedTriggerConfiguration) Validate() error {
	return configuration.MessageMatcher.validate()
}

// Validate checks reply Trigger configuration.
func (configuration ReplyReceivedTriggerConfiguration) Validate() error {
	return configuration.ReplyMatcher.validate()
}

func (matcher MessageMatcher) validate() error {
	seen := make(map[string]bool, len(matcher.SenderEmails))
	for _, sender := range matcher.SenderEmails {
		address, err := mail.ParseAddress(sender)
		if err != nil || address.Address == "" {
			return fmt.Errorf("Gmail sender email is invalid")
		}
		canonical := strings.ToLower(address.Address)
		if seen[canonical] {
			return fmt.Errorf("Gmail sender emails must be unique")
		}
		seen[canonical] = true
	}
	return nil
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

func (source *messagePollingTriggerSource) Run(ctx context.Context, target sdkgo.TriggerTarget[MessageEvent]) error {
	for {
		if err := source.scan(ctx, target); err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		timer := time.NewTimer(source.client.pollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// messageListing is one inbox list call, delivered later by deliver.
type messageListing struct {
	credentials Credentials
	messages    gmailMessageList
}

// scan lists the inbox and delivers the listed messages.
func (source *messagePollingTriggerSource) scan(ctx context.Context, target sdkgo.TriggerTarget[MessageEvent]) error {
	listing, err := source.list(ctx)
	if err != nil {
		return err
	}
	return source.deliver(ctx, target, listing)
}

// list reads the newest inbox page that matches the source's search query.
func (source *messagePollingTriggerSource) list(ctx context.Context) (messageListing, error) {
	credentials, err := source.client.credentials.Resolve(sdkgo.Call{Connection: source.connection})
	if err != nil || credentials.Validate() != nil {
		return messageListing{}, source.pollFailed(ctx, fmt.Errorf("Gmail Trigger credentials are unavailable"))
	}
	listed, err := source.listMessages(ctx, credentials)
	if err != nil {
		return messageListing{}, source.pollFailed(ctx, err)
	}
	return messageListing{credentials: credentials, messages: listed}, nil
}

// pollFailed logs a poll that ends early because Gmail or the inbox failed and returns err. The next
// poll, one poll interval later, retries. attempt counts the polls that failed in a row.
func (source *messagePollingTriggerSource) pollFailed(ctx context.Context, err error, attrs ...slog.Attr) error {
	if ctx.Err() == nil {
		source.pollFailures++
		source.log(ctx, slog.LevelWarn, "gmail poll failed; retrying", append(attrs, slog.Int("attempt", source.pollFailures),
			slog.Duration("delay", source.client.pollInterval), slog.String("error", withoutRequestQuery(err).Error()))...)
	}
	return err
}

// withoutRequestQuery drops the query string, such as the configured search query, from an HTTP client
// error before it is logged.
func withoutRequestQuery(err error) error {
	urlError, ok := err.(*url.Error)
	if !ok {
		return err
	}
	parsed, parseErr := url.Parse(urlError.URL)
	if parseErr != nil {
		return &url.Error{Op: urlError.Op, URL: "Gmail API", Err: urlError.Err}
	}
	parsed.RawQuery = ""
	return &url.Error{Op: urlError.Op, URL: parsed.String(), Err: urlError.Err}
}

// deliver hands target the event that failed on an earlier poll, if any, and then every new matching
// message in listing, oldest first. It stops at the first failure, so the next poll retries that event
// before any later message. The failed event is retried even after newer mail pushes it off the listed
// page, so it is neither lost nor overtaken.
func (source *messagePollingTriggerSource) deliver(ctx context.Context, target sdkgo.TriggerTarget[MessageEvent], listing messageListing) error {
	if source.retry != nil {
		if err := source.handle(ctx, target, source.retry.event); err != nil {
			return err
		}
	}
	credentials, listed := listing.credentials, listing.messages
	current := make(map[string]bool, len(listed.Messages))
	for index := len(listed.Messages) - 1; index >= 0; index-- {
		reference := listed.Messages[index]
		if reference.ID == "" {
			continue
		}
		current[reference.ID] = true
		if source.delivered[reference.ID] {
			continue
		}
		messageAttrs := []slog.Attr{slog.String("thread_id", reference.ThreadID), slog.String("event_id", reference.ID)}
		resource, _, err := source.client.readMessage(ctx, credentials, reference.ID, "metadata")
		if err != nil {
			return source.pollFailed(ctx, err, messageAttrs...)
		}
		message, err := decodeGmailMessage(resource)
		if err != nil {
			return source.pollFailed(ctx, err, messageAttrs...)
		}
		isReply := strings.TrimSpace(message.InReplyTo) != "" || strings.TrimSpace(message.References) != ""
		if reason := source.ignoreReason(isReply, message); reason != "" {
			source.log(ctx, slog.LevelDebug, "trigger event ignored", append(messageAttrs, slog.String("reason", reason))...)
			continue
		}
		event := sdkgo.TriggerEvent[MessageEvent]{
			ID: message.MessageID, OccurredAt: message.ReceivedAt,
			Payload: MessageEvent{
				PrimaryEmail: credentials.PrimaryEmail, MessageID: message.MessageID, ThreadID: message.ThreadID,
				From: message.From, Subject: message.Subject, Snippet: message.Snippet, ReceivedAt: message.ReceivedAt, IsReply: isReply,
			},
		}
		if err := sdkgo.PrepareTriggerDelivery(ctx, target, event); err != nil {
			return source.pollFailed(ctx, err, messageAttrs...)
		}
		// An undeliverable event is consumed; any other failure ends this scan so the next poll retries it.
		if err := source.handle(ctx, target, event); err != nil {
			return err
		}
	}
	source.delivered = current
	source.pollFailures = 0
	return nil
}

// handle makes one delivery attempt and logs its outcome. It returns nil when target consumes the event,
// and otherwise keeps the event as the source's retry. A durable inbox or a NewTrigger runner logs the
// undeliverable events it consumes itself and returns nil for them; TriggerAttempt reports that, so the
// source does not also log such an event as delivered after a retry.
func (source *messagePollingTriggerSource) handle(
	ctx context.Context,
	target sdkgo.TriggerTarget[MessageEvent],
	event sdkgo.TriggerEvent[MessageEvent],
) error {
	failures := 0
	if source.retry != nil && source.retry.event.ID == event.ID {
		failures = source.retry.failures
	}
	if source.delivered == nil {
		source.delivered = make(map[string]bool)
	}
	threadID := slog.String("thread_id", event.Payload.ThreadID)
	messageAttrs := []slog.Attr{threadID, slog.String("event_id", event.ID)}
	// The thread ID also reaches the records of the inbox and the Dex targets inside the attempt.
	attemptCtx, skippedInside := sdkgo.TriggerAttempt(sdkgo.ContextWithTriggerLogAttrs(ctx, threadID))
	err := target.HandleTrigger(attemptCtx, event)
	switch {
	case err == nil:
		if failures > 0 && !skippedInside() {
			source.log(ctx, slog.LevelInfo, "trigger delivered after retry", append(messageAttrs, slog.Int("attempts", failures+1))...)
		}
	case sdkgo.IsTriggerUndeliverable(err):
		source.log(ctx, slog.LevelWarn, "trigger event skipped: undeliverable", append(messageAttrs, triggerErrorAttrs(err)...)...)
	case ctx.Err() != nil:
		return err
	default:
		// Gmail answered, so the poll itself did not fail.
		source.pollFailures = 0
		source.retry = &pendingMessageRetry{event: event, failures: failures + 1}
		source.log(ctx, slog.LevelWarn, "trigger delivery failed; retrying", append(append(messageAttrs,
			slog.Int("attempt", failures+1), slog.Duration("delay", source.client.pollInterval)), triggerErrorAttrs(err)...)...)
		return err
	}
	source.retry = nil
	// A consumed retry that Gmail still lists must not be delivered again from the listing.
	source.delivered[event.ID] = true
	return nil
}

// triggerErrorAttrs returns a "flow_id" attribute when err is a Dex error about one Flow, followed by the
// "error" attribute, like the sdkgo delivery records. The error message loses any URL query string.
func triggerErrorAttrs(err error) []slog.Attr {
	message := slog.String("error", withoutRequestQuery(err).Error())
	var serviceError *dex.ServiceError
	if errors.As(err, &serviceError) && serviceError.FlowID != "" {
		return []slog.Attr{slog.String("flow_id", serviceError.FlowID), message}
	}
	return []slog.Attr{message}
}

// ignoreReason explains why the source does not deliver a listed message, or returns "" when it matches.
func (source *messagePollingTriggerSource) ignoreReason(isReply bool, message Message) string {
	switch {
	case source.requiresReply && !isReply:
		return "not_a_reply"
	case !source.requiresReply && isReply:
		return "not_a_root"
	case !source.matcher.matches(message):
		return "matcher_mismatch"
	}
	return ""
}

// log writes one record with the source's binding attributes when the client's logger is enabled for level.
// It builds the record itself, so a handler with AddSource reports the function that called log rather
// than this helper.
func (source *messagePollingTriggerSource) log(ctx context.Context, level slog.Level, message string, attrs ...slog.Attr) {
	logger := source.client.triggerLogger()
	if !logger.Enabled(ctx, level) {
		return
	}
	var pcs [1]uintptr
	// Skip runtime.Callers and log.
	runtime.Callers(2, pcs[:])
	record := slog.NewRecord(time.Now(), level, message, pcs[0])
	record.AddAttrs(
		slog.String("connector", ConnectorID), slog.String("connection", source.connection.Name), slog.String("trigger", source.triggerName),
	)
	if source.bindingName != "" {
		record.AddAttrs(slog.String("binding", source.bindingName))
	}
	record.AddAttrs(attrs...)
	_ = logger.Handler().Handle(ctx, record)
}

// triggerLogger returns the configured logger or, as of the call, slog.Default().
func (client *Client) triggerLogger() *slog.Logger {
	if client != nil && client.logger != nil {
		return client.logger
	}
	return slog.Default()
}

func (source *messagePollingTriggerSource) listMessages(ctx context.Context, credentials Credentials) (gmailMessageList, error) {
	query := url.Values{"labelIds": {"INBOX"}, "maxResults": {fmt.Sprintf("%d", source.client.pollPageSize)}}
	if strings.TrimSpace(source.searchQuery) != "" {
		query.Set("q", source.searchQuery)
	}
	target := strings.TrimRight(source.client.endpoint.String(), "/") + "/users/me/messages?" + query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return gmailMessageList{}, err
	}
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	response, err := source.client.httpClient.Do(request)
	if err != nil {
		return gmailMessageList{}, err
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(response.Body, source.client.maxResponseBytes+1))
	if err != nil || int64(len(contents)) > source.client.maxResponseBytes || response.StatusCode < 200 || response.StatusCode >= 300 {
		return gmailMessageList{}, fmt.Errorf("Gmail message list is unavailable")
	}
	var result gmailMessageList
	if err := json.Unmarshal(contents, &result); err != nil {
		return gmailMessageList{}, err
	}
	return result, nil
}

func (matcher MessageMatcher) matches(message Message) bool {
	if matcher.MessageContains != "" {
		haystack := strings.ToLower(message.Subject + "\n" + message.Snippet)
		if !strings.Contains(haystack, strings.ToLower(matcher.MessageContains)) {
			return false
		}
	}
	if len(matcher.SenderEmails) == 0 {
		return true
	}
	address, err := mail.ParseAddress(message.From)
	if err != nil {
		return false
	}
	for _, allowed := range matcher.SenderEmails {
		allowedAddress, err := mail.ParseAddress(allowed)
		if err == nil && strings.EqualFold(address.Address, allowedAddress.Address) {
			return true
		}
	}
	return false
}
