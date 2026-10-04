// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

const (
	// helpScoutSignatureHeader carries the base64 HMAC-SHA1 of the raw body keyed with the webhook secret.
	helpScoutSignatureHeader = "X-HelpScout-Signature"
	// helpScoutEventHeader names the webhook event, such as convo.created.
	helpScoutEventHeader = "X-HelpScout-Event"

	// WebhookEventConversationCreated is sent when a conversation is created.
	WebhookEventConversationCreated = "convo.created"
	// WebhookEventConversationAssigned is sent when a conversation's assignee changes.
	WebhookEventConversationAssigned = "convo.assigned"
	// WebhookEventConversationStatusUpdated is sent when a conversation's status changes.
	WebhookEventConversationStatusUpdated = "convo.status"
	// WebhookEventConversationTagsUpdated is sent when a conversation's tags change.
	WebhookEventConversationTagsUpdated = "convo.tags"
	// WebhookEventConversationCustomFieldsUpdated is sent when a conversation's custom fields change.
	WebhookEventConversationCustomFieldsUpdated = "convo.custom-fields"
	// WebhookEventConversationMoved is sent when a conversation moves to another inbox.
	WebhookEventConversationMoved = "convo.moved"
	// WebhookEventConversationMerged is sent when a conversation is merged into another.
	WebhookEventConversationMerged = "convo.merged"
	// WebhookEventCustomerReplyCreated is sent when a customer replies.
	WebhookEventCustomerReplyCreated = "convo.customer.reply.created"
	// WebhookEventAgentReplyCreated is sent when a user replies; Help Scout omits auto replies.
	WebhookEventAgentReplyCreated = "convo.agent.reply.created"
	// WebhookEventNoteCreated is sent when a user adds an internal note.
	WebhookEventNoteCreated = "convo.note.created"
)

var (
	webhookEventNamePattern = regexp.MustCompile(`^[a-z][a-z0-9.-]{0,63}$`)

	errHelpScoutSignatureInvalid = errors.New("X-HelpScout-Signature is missing, malformed, or does not match")
	errHelpScoutWebhookEvent     = errors.New("X-HelpScout-Event is missing or malformed")
)

// ConversationWebhookEvents returns the Help Scout webhook events whose body is a conversation, which the
// conversationEvent Trigger decodes. Help Scout documents each with a v2 Conversation object body.
func ConversationWebhookEvents() []string {
	return []string{
		WebhookEventConversationCreated, WebhookEventConversationAssigned, WebhookEventConversationStatusUpdated,
		WebhookEventConversationTagsUpdated, WebhookEventConversationCustomFieldsUpdated, WebhookEventConversationMoved,
		WebhookEventConversationMerged, WebhookEventCustomerReplyCreated, WebhookEventAgentReplyCreated, WebhookEventNoteCreated,
	}
}

// ConversationEventTriggerConfiguration filters the Help Scout webhook events one binding records. The
// zero value records every conversation event of every inbox. It reduces what a binding stores; the
// application's TriggerFilter remains its admission rule.
type ConversationEventTriggerConfiguration struct {
	// Events lists conversation webhook events from ConversationWebhookEvents; empty accepts every one.
	Events []string `json:"events,omitempty"`
	// MailboxID accepts only conversations of this inbox, such as the mailboxPicker unit's mailboxId; zero
	// accepts every inbox.
	MailboxID int64 `json:"mailboxId,omitempty"`
}

// ConversationEvent is one verified Help Scout conversation webhook. The Trigger event ID is the event,
// the conversation ID, and the first 32 hex digits of the body's SHA-256, such as
// convo.created:123456:0f1e..., because Help Scout sends no delivery ID: a redelivery of the same body
// keeps its ID, and a later event about the same conversation has another. TriggerEvent.OccurredAt is when
// this process received the request, because Help Scout sends no event time.
type ConversationEvent struct {
	// Event is the X-HelpScout-Event value, such as convo.customer.reply.created.
	Event string `json:"event"`
	// Conversation is the conversation as the webhook body described it; its threads are not included, so
	// a Flow reads them with getConversation.
	Conversation Conversation `json:"conversation"`
}

// Validate checks that Events holds distinct conversation webhook events and MailboxID is not negative.
func (configuration ConversationEventTriggerConfiguration) Validate() error {
	seen := make(map[string]bool, len(configuration.Events))
	for _, event := range configuration.Events {
		if !slices.Contains(ConversationWebhookEvents(), event) {
			return fmt.Errorf("conversationEvent event %q is not a Help Scout conversation webhook event", event)
		}
		if seen[event] {
			return fmt.Errorf("conversationEvent events lists %q twice", event)
		}
		seen[event] = true
	}
	if configuration.MailboxID < 0 {
		return errors.New("conversationEvent mailboxId must be a positive Help Scout inbox ID, or zero for every inbox")
	}
	return nil
}

// acceptsEvent applies the binding's event and inbox filters.
func (configuration ConversationEventTriggerConfiguration) acceptsEvent(event sdkgo.TriggerEvent[ConversationEvent]) bool {
	if len(configuration.Events) > 0 && !slices.Contains(configuration.Events, event.Payload.Event) {
		return false
	}
	return configuration.MailboxID == 0 || configuration.MailboxID == event.Payload.Conversation.MailboxID
}

// ConversationEventWebhookHandler returns the connection's webhook endpoint for an application to mount at
// the public HTTPS URL of its Help Scout webhook. Every conversationEvent Trigger built from this Connection
// feeds from it, and it answers 503 while none of them runs, so Help Scout retries.
func (connection Connection) ConversationEventWebhookHandler() (http.Handler, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	return connection.client.conversationEventWebhookEndpoint(connection.reference)
}

func (client *Client) conversationEventTriggerSource(
	connection sdkgo.ConnectionRef,
	configuration ConversationEventTriggerConfiguration,
) sdkgo.TriggerSource[ConversationEvent] {
	if err := configuration.Validate(); err != nil {
		panic(err)
	}
	endpoint, err := client.conversationEventWebhookEndpoint(connection)
	if err != nil {
		panic(err)
	}
	return endpoint.NewSource(configuration.acceptsEvent)
}

// conversationEventWebhookEndpoint returns the connection's shared endpoint, creating it on first use.
func (client *Client) conversationEventWebhookEndpoint(
	connection sdkgo.ConnectionRef,
) (*webhooktrigger.Endpoint[Credentials, ConversationEvent], error) {
	client.conversationEventEndpointsMu.Lock()
	defer client.conversationEventEndpointsMu.Unlock()
	if endpoint, isFound := client.conversationEventEndpoints[connection]; isFound {
		return endpoint, nil
	}
	endpoint, err := webhooktrigger.NewEndpoint(webhooktrigger.EndpointConfig[Credentials, ConversationEvent]{
		ConnectorID: ConnectorID, TriggerName: ConversationEventTriggerDefinition.Trigger.TriggerName,
		Connection: connection, Credentials: client.credentials, CredentialRefresh: expiredRecordRefreshDriver{tokenDriver: client.refreshDriver},
		MaxBodyBytes: client.webhookMaxBodyBytes, VerifyRequest: verifyConversationWebhookRequest,
		DecodeEvent: decodeConversationWebhookRequest, Now: client.now, Logger: client.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("Help Scout webhook endpoint: %w", err)
	}
	client.conversationEventEndpoints[connection] = endpoint
	return endpoint, nil
}

// expiredRecordRefreshDriver renews only a record project storage refuses as expired; verification needs only the secret.
type expiredRecordRefreshDriver struct{ tokenDriver *CredentialRefreshDriver }

// RefreshRequired reports whether the stored expiry has passed.
func (expiredRecordRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return state.ExpiresAt != nil && !state.Now.Before(*state.ExpiresAt)
}

// Refresh obtains a new access token with the connection's CredentialRefreshDriver.
func (driver expiredRecordRefreshDriver) Refresh(
	ctx context.Context, state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	return driver.tokenDriver.Refresh(ctx, state)
}

// verifyConversationWebhookRequest answers 503 without a webhook secret, so Help Scout keeps retrying.
func verifyConversationWebhookRequest(request webhooktrigger.Request, credentials Credentials) error {
	secret := credentials.WebhookSecret.Reveal()
	if secret == "" {
		return webhooktrigger.ErrVerificationUnavailable
	}
	return verifyHelpScoutSignature(request.Body, request.Header.Get(helpScoutSignatureHeader), secret)
}

// verifyHelpScoutSignature compares base64(HMAC-SHA1(body, secret)) in constant time; Help Scout signs no timestamp.
func verifyHelpScoutSignature(body []byte, header string, secret string) error {
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(header))
	if err != nil || len(signature) != sha1.Size {
		return errHelpScoutSignatureInvalid
	}
	mac := hmac.New(sha1.New, []byte(secret))
	_, _ = mac.Write(body)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return errHelpScoutSignatureInvalid
	}
	return nil
}

// decodeConversationWebhookRequest acknowledges, without a record, events outside ConversationWebhookEvents.
func decodeConversationWebhookRequest(request webhooktrigger.Request) (sdkgo.TriggerEvent[ConversationEvent], bool, error) {
	event := strings.TrimSpace(request.Header.Get(helpScoutEventHeader))
	if !webhookEventNamePattern.MatchString(event) {
		return sdkgo.TriggerEvent[ConversationEvent]{}, false, errHelpScoutWebhookEvent
	}
	if !slices.Contains(ConversationWebhookEvents(), event) {
		return sdkgo.TriggerEvent[ConversationEvent]{}, false, nil
	}
	var wire helpScoutConversationWire
	if err := decodeHelpScoutJSON(request.Body, &wire); err != nil {
		return sdkgo.TriggerEvent[ConversationEvent]{}, false, err
	}
	conversation, err := decodeConversation(wire)
	if err != nil {
		return sdkgo.TriggerEvent[ConversationEvent]{}, false, err
	}
	bodyDigest := sha256.Sum256(request.Body)
	return sdkgo.TriggerEvent[ConversationEvent]{
		ID:         event + ":" + strconv.FormatInt(conversation.ID, 10) + ":" + hex.EncodeToString(bodyDigest[:16]),
		OccurredAt: request.ReceivedAt.UTC(),
		Payload:    ConversationEvent{Event: event, Conversation: conversation},
	}, true, nil
}
