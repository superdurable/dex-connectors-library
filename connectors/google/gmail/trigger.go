// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
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
	if err := configuration.Validate(); err != nil {
		panic(err)
	}
	return &messagePollingTriggerSource{
		client: client, connection: connection, searchQuery: configuration.SearchQuery,
		matcher: configuration.MessageMatcher, delivered: make(map[string]bool),
	}
}

func (client *Client) replyReceivedTriggerSource(connection sdkgo.ConnectionRef, configuration ReplyReceivedTriggerConfiguration) sdkgo.TriggerSource[MessageEvent] {
	if err := configuration.Validate(); err != nil {
		panic(err)
	}
	return &messagePollingTriggerSource{
		client: client, connection: connection, searchQuery: configuration.SearchQuery,
		matcher: configuration.ReplyMatcher, requiresReply: true, delivered: make(map[string]bool),
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

func (source *messagePollingTriggerSource) scan(ctx context.Context, target sdkgo.TriggerTarget[MessageEvent]) error {
	credentials, err := source.client.credentials.Resolve(sdkgo.Call{Connection: source.connection})
	if err != nil || credentials.Validate() != nil {
		return fmt.Errorf("Gmail Trigger credentials are unavailable")
	}
	listed, err := source.listMessages(ctx, credentials)
	if err != nil {
		return err
	}
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
		resource, _, err := source.client.readMessage(ctx, credentials, reference.ID, "metadata")
		if err != nil {
			return err
		}
		message, err := decodeGmailMessage(resource)
		if err != nil {
			return err
		}
		isReply := strings.TrimSpace(message.InReplyTo) != "" || strings.TrimSpace(message.References) != ""
		if isReply != source.requiresReply || !source.matcher.matches(message) {
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
			return err
		}
		if err := target.HandleTrigger(ctx, event); err != nil {
			return err
		}
	}
	source.delivered = current
	return nil
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
