// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

const (
	hubSignatureHeader = "X-Hub-Signature"
	hubSignaturePrefix = "sha1="

	// maximumNotificationIDBytes bounds notification IDs, which applications embed in Flow IDs.
	maximumNotificationIDBytes = 256
)

// Conversation webhook topics whose notification item is a Conversation. The conversationEvent Trigger
// decodes these and answers 200 to every other topic, including Intercom's periodic ping, without
// recording it.
const (
	// TopicConversationUserCreated is a conversation a user, lead, or visitor started.
	TopicConversationUserCreated = "conversation.user.created"
	// TopicConversationUserReplied is a reply from the customer.
	TopicConversationUserReplied = "conversation.user.replied"
	// TopicConversationAdminReplied is a reply from a teammate.
	TopicConversationAdminReplied = "conversation.admin.replied"
	// TopicConversationAdminSingleCreated is a one-to-one conversation a teammate started.
	TopicConversationAdminSingleCreated = "conversation.admin.single.created"
	// TopicConversationAdminAssigned is an assignment by a teammate.
	TopicConversationAdminAssigned = "conversation.admin.assigned"
	// TopicConversationAdminOpenAssigned is an assignment of an open conversation.
	TopicConversationAdminOpenAssigned = "conversation.admin.open.assigned"
	// TopicConversationAdminNoted is an internal note from a teammate.
	TopicConversationAdminNoted = "conversation.admin.noted"
	// TopicConversationAdminClosed is a conversation a teammate closed.
	TopicConversationAdminClosed = "conversation.admin.closed"
	// TopicConversationAdminOpened is a conversation a teammate reopened.
	TopicConversationAdminOpened = "conversation.admin.opened"
	// TopicConversationAdminSnoozed is a conversation a teammate snoozed.
	TopicConversationAdminSnoozed = "conversation.admin.snoozed"
	// TopicConversationAdminUnsnoozed is a snoozed conversation that reopened.
	TopicConversationAdminUnsnoozed = "conversation.admin.unsnoozed"
	// TopicConversationOperatorReplied is a reply from Fin or another bot.
	TopicConversationOperatorReplied = "conversation.operator.replied"
	// TopicConversationPriorityUpdated is a priority change.
	TopicConversationPriorityUpdated = "conversation.priority.updated"
	// TopicConversationRatingAdded is a customer's conversation rating.
	TopicConversationRatingAdded = "conversation.rating.added"
)

var supportedConversationTopics = []string{
	TopicConversationUserCreated, TopicConversationUserReplied, TopicConversationAdminReplied, TopicConversationAdminSingleCreated,
	TopicConversationAdminAssigned, TopicConversationAdminOpenAssigned, TopicConversationAdminNoted, TopicConversationAdminClosed,
	TopicConversationAdminOpened, TopicConversationAdminSnoozed, TopicConversationAdminUnsnoozed, TopicConversationOperatorReplied,
	TopicConversationPriorityUpdated, TopicConversationRatingAdded,
}

// ConversationEventTopics returns every topic the conversationEvent Trigger decodes, in a stable order.
func ConversationEventTopics() []string {
	return slices.Clone(supportedConversationTopics)
}

// ConversationEventTriggerConfiguration selects the topics one conversationEvent binding accepts. An
// empty list accepts every topic ConversationEventTopics returns. A topic the binding rejects is still
// answered 200. The topics a binding can receive are those subscribed in the Intercom app's Configure >
// Webhooks page. NewConversationEventTrigger panics for an invalid configuration, so call Validate on
// untrusted values.
type ConversationEventTriggerConfiguration struct {
	// Topics lists accepted webhook topics, such as conversation.user.created; empty accepts all supported topics.
	Topics []string `json:"topics,omitempty"`
}

// Validate checks that every configured topic is supported and listed once.
func (configuration ConversationEventTriggerConfiguration) Validate() error {
	seen := make(map[string]bool, len(configuration.Topics))
	for _, topic := range configuration.Topics {
		if !slices.Contains(supportedConversationTopics, topic) {
			return fmt.Errorf("Intercom webhook topic %q is not a supported conversation topic", topic)
		}
		if seen[topic] {
			return fmt.Errorf("Intercom webhook topic %q is listed twice", topic)
		}
		seen[topic] = true
	}
	return nil
}

func (configuration ConversationEventTriggerConfiguration) acceptsTopic(topic string) bool {
	if len(configuration.Topics) == 0 {
		return slices.Contains(supportedConversationTopics, topic)
	}
	return slices.Contains(configuration.Topics, topic)
}

// ConversationEvent is one verified Intercom conversation webhook notification. The Trigger event's ID
// is Intercom's notification ID, which a redelivery of the same notification reuses.
type ConversationEvent struct {
	// NotificationID is Intercom's notification ID, such as notif_ccd8a4d0-f965-11e3-a367-c779cae3e1b3.
	NotificationID string `json:"notificationId"`
	// Topic is the webhook topic, such as conversation.user.created.
	Topic string `json:"topic"`
	// WorkspaceID is Intercom's app_id of the workspace the event happened in. An app installed in
	// several workspaces receives every workspace's notifications at one URL.
	WorkspaceID string `json:"workspaceId,omitempty"`
	// Conversation is the conversation as the notification carried it, without its parts or first-message body.
	Conversation Conversation `json:"conversation"`
	// NotifiedAt is when Intercom created the notification. Intercom does not guarantee delivery order,
	// so compare it, not arrival order. It is also the Trigger event's OccurredAt.
	NotifiedAt time.Time `json:"notifiedAt"`
}

type intercomNotificationWire struct {
	Type      string      `json:"type"`
	ID        string      `json:"id"`
	Topic     string      `json:"topic"`
	AppID     string      `json:"app_id"`
	CreatedAt unixSeconds `json:"created_at"`
	Data      struct {
		Item json.RawMessage `json:"item"`
	} `json:"data"`
}

// headValidatingHandler answers the HEAD request Intercom sends to validate a webhook URL when it is saved.
type headValidatingHandler struct {
	next http.Handler
}

// ServeHTTP answers HEAD with 200 and passes every other request to the webhook endpoint.
func (handler headValidatingHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodHead {
		response.WriteHeader(http.StatusOK)
		return
	}
	handler.next.ServeHTTP(response, request)
}

// verifyConversationEventRequest checks X-Hub-Signature: sha1= and the hex HMAC-SHA1 of the body under the client secret.
func verifyConversationEventRequest(request webhooktrigger.Request, credentials Credentials) error {
	secret := credentials.ClientSecret.Reveal()
	if secret == "" {
		return fmt.Errorf("Intercom client_secret is not configured: %w", webhooktrigger.ErrVerificationUnavailable)
	}
	encoded, isPrefixed := strings.CutPrefix(strings.TrimSpace(request.Header.Get(hubSignatureHeader)), hubSignaturePrefix)
	if !isPrefixed {
		return errors.New("X-Hub-Signature is missing or does not start with sha1=")
	}
	signature, err := hex.DecodeString(encoded)
	if err != nil || len(signature) != sha1.Size {
		return errors.New("X-Hub-Signature is not a hex SHA-1 signature")
	}
	mac := hmac.New(sha1.New, []byte(secret))
	_, _ = mac.Write(request.Body) // A hash write never fails.
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return errors.New("X-Hub-Signature does not match the body")
	}
	return nil
}

// decodeConversationEventRequest decodes a verified notification; another topic is valid but unhandled.
func decodeConversationEventRequest(request webhooktrigger.Request) (sdkgo.TriggerEvent[ConversationEvent], bool, error) {
	var notification intercomNotificationWire
	if err := json.Unmarshal(request.Body, &notification); err != nil {
		return sdkgo.TriggerEvent[ConversationEvent]{}, false, errors.New("Intercom notification is not JSON")
	}
	if notification.Type != "notification_event" || notification.Topic == "" || !isPrintableIdentifier(notification.ID, maximumNotificationIDBytes) {
		return sdkgo.TriggerEvent[ConversationEvent]{}, false, errors.New("Intercom notification envelope is invalid")
	}
	if !slices.Contains(supportedConversationTopics, notification.Topic) {
		return sdkgo.TriggerEvent[ConversationEvent]{}, false, nil
	}
	var wire intercomConversationWire
	if err := json.Unmarshal(notification.Data.Item, &wire); err != nil {
		return sdkgo.TriggerEvent[ConversationEvent]{}, false, errors.New("Intercom notification item is not a conversation")
	}
	conversation, err := decodeConversationWire(wire, false)
	if err != nil {
		return sdkgo.TriggerEvent[ConversationEvent]{}, false, fmt.Errorf("Intercom notification item: %w", err)
	}
	notifiedAt := notification.CreatedAt.time()
	if notifiedAt.IsZero() {
		notifiedAt = request.ReceivedAt.UTC()
	}
	return sdkgo.TriggerEvent[ConversationEvent]{
		ID: notification.ID, OccurredAt: notifiedAt,
		Payload: ConversationEvent{
			NotificationID: notification.ID, Topic: notification.Topic, WorkspaceID: notification.AppID,
			Conversation: conversation, NotifiedAt: notifiedAt,
		},
	}, true, nil
}

func isPrintableIdentifier(value string, maximumBytes int) bool {
	if value == "" || len(value) > maximumBytes {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] <= ' ' || value[index] > '~' {
			return false
		}
	}
	return true
}
