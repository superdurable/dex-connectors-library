// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pubsub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// PublishMessageInput sends one UTF-8 text message.
type PublishMessageInput struct {
	// Topic is the full topic resource name.
	Topic string `json:"topic"`
	// Text is the non-empty UTF-8 payload, encoded as base64 on the wire.
	Text string `json:"text"`
	// Attributes are optional message metadata.
	Attributes map[string]string `json:"attributes,omitempty"`
}

// PublishedMessage identifies a message accepted by Pub/Sub.
type PublishedMessage struct {
	// MessageID is the provider-assigned message identity.
	MessageID string `json:"messageId"`
}

// PullMessagesInput selects a bounded batch from a pull subscription.
type PullMessagesInput struct {
	// Subscription is the full subscription resource name.
	Subscription string `json:"subscription"`
	// MaxMessages is the maximum messages to return, from 1 through 1000.
	MaxMessages int `json:"maxMessages"`
}

// PulledMessage is the provider's base64-encoded message payload and metadata.
type PulledMessage struct {
	// MessageID is the provider's message identity.
	MessageID string `json:"messageId"`
	// Data is the base64-encoded message payload.
	Data string `json:"data,omitempty"`
	// Attributes are provider message metadata.
	Attributes map[string]string `json:"attributes,omitempty"`
}

// ReceivedMessage pairs a message with its temporary acknowledgement ID.
type ReceivedMessage struct {
	// AckID is passed to AcknowledgeMessages before its deadline.
	AckID string `json:"ackId"`
	// Message is the delivered payload.
	Message PulledMessage `json:"message"`
}

// PulledMessages is one bounded pull response.
type PulledMessages struct {
	// ReceivedMessages contains the currently available deliveries.
	ReceivedMessages []ReceivedMessage `json:"receivedMessages"`
}

// AcknowledgeMessagesInput confirms a bounded set of deliveries.
type AcknowledgeMessagesInput struct {
	// Subscription is the full subscription resource name.
	Subscription string `json:"subscription"`
	// AckIDs are temporary IDs returned by PullMessages.
	AckIDs []string `json:"ackIds"`
}

// AcknowledgedMessages reports how many IDs were submitted.
type AcknowledgedMessages struct {
	// AcknowledgedCount is the number of IDs submitted to Pub/Sub.
	AcknowledgedCount int `json:"acknowledgedCount"`
}

// PublishMessageOperation sends one message with a bounded repeat budget.
type PublishMessageOperation struct{ client *Client }

// Definition returns the generated publish contract.
func (PublishMessageOperation) Definition() sdkgo.MutationDefinition { return PublishMessageDefinition }

// IdempotencyKey identifies the logical publish call, though Pub/Sub has no wire key.
func (PublishMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ PublishMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// PublishMessage binds the publish Mutation to the client.
func (client *Client) PublishMessage() PublishMessageOperation {
	return PublishMessageOperation{client}
}

// Invoke publishes once per fenced dispatch and stops at the send cap.
func (operation PublishMessageOperation) Invoke(call sdkgo.Call, input PublishMessageInput) sdkgo.MutationAttempt[PublishedMessage] {
	if !validTopicName(input.Topic) || (input.Text == "" && len(input.Attributes) == 0) || len(input.Text) > 1048576 || !validMessageAttributes(input.Attributes) {
		return topicManagementMutationBranch(PublishMessageBranchDefect, PublishedMessage{}, sdkgo.FailureValidation, "Pub/Sub message input is invalid")
	}
	fence, err := singledispatch.NewFence(call, PublishMessageDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return topicManagementMutationBranch(PublishMessageBranchDefect, PublishedMessage{}, sdkgo.FailureLocalDefect, "Pub/Sub publish checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return topicManagementMutationBranch(PublishMessageBranchDefect, PublishedMessage{}, sdkgo.FailureLocalDefect, "Pub/Sub publish checkpoint is invalid")
	}
	if decision.Marker.ProviderID != "" {
		return sdkgo.NewMutationBranch(PublishMessageBranchPublished, PublishedMessage{MessageID: decision.Marker.ProviderID}, nil, topicManagementReceipt(call, decision.Marker.ProviderID))
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return topicManagementMutationBranch(PublishMessageBranchUncertain, PublishedMessage{}, sdkgo.FailureAvailability, "Pub/Sub publish may have succeeded but no message ID was returned before the send cap")
	}
	body, _ := json.Marshal(struct {
		Messages []struct {
			Data       string            `json:"data,omitempty"`
			Attributes map[string]string `json:"attributes,omitempty"`
		} `json:"messages"`
	}{
		Messages: []struct {
			Data       string            `json:"data,omitempty"`
			Attributes map[string]string `json:"attributes,omitempty"`
		}{{Data: base64.StdEncoding.EncodeToString([]byte(input.Text)), Attributes: input.Attributes}},
	})
	var response providerResponse
	if err := fence.Dispatch(func(dispatchContext context.Context) error {
		response = operation.client.requestWithContext(dispatchContext, call, http.MethodPost, "/v1/"+input.Topic+":publish", body)
		return nil
	}); err != nil {
		return sdkgo.NewMutationRetry[PublishedMessage](topicManagementFailure("publishMessage", sdkgo.FailureAvailability, "Pub/Sub publish could not be dispatched safely"), 0)
	}
	if failure := topicManagementResponseFailure(response); failure != nil {
		if failure.Kind == sdkgo.FailureAuthentication {
			return topicManagementMutationBranch(PublishMessageBranchDefect, PublishedMessage{}, failure.Kind, failure.Message)
		}
		return sdkgo.NewMutationRetry[PublishedMessage](*failure, 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[PublishedMessage](topicManagementFailure("publishMessage", sdkgo.FailureAvailability, "Pub/Sub publish refusal checkpoint could not be recorded"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[PublishedMessage](topicManagementFailure("publishMessage", kind, "Pub/Sub temporarily refused the publish"), response.retryAfter)
	}
	if response.status >= 500 {
		return sdkgo.NewMutationRetry[PublishedMessage](topicManagementFailure("publishMessage", sdkgo.FailureAvailability, "Pub/Sub publish outcome is unknown"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		if decision.Kind != singledispatch.SendAllowed {
			return topicManagementMutationBranch(PublishMessageBranchUncertain, PublishedMessage{}, sdkgo.FailureAvailability, "Pub/Sub rejected a later attempt, but an earlier publish may have succeeded")
		}
		return topicManagementMutationBranch(PublishMessageBranchProviderRejected, PublishedMessage{}, sdkgo.FailureProviderRejection, "Pub/Sub rejected publish with HTTP "+strconv.Itoa(response.status))
	}
	var published struct {
		MessageIDs []string `json:"messageIds"`
	}
	if json.Unmarshal(response.body, &published) != nil || len(published.MessageIDs) != 1 || published.MessageIDs[0] == "" {
		return sdkgo.NewMutationRetry[PublishedMessage](topicManagementFailure("publishMessage", sdkgo.FailureProtocol, "Pub/Sub accepted publish but returned no message ID"), 0)
	}
	if err := fence.RecordProviderID(published.MessageIDs[0]); err != nil {
		return sdkgo.NewMutationRetry[PublishedMessage](topicManagementFailure("publishMessage", sdkgo.FailureAvailability, "Pub/Sub message ID checkpoint could not be recorded"), 0)
	}
	return sdkgo.NewMutationBranch(PublishMessageBranchPublished, PublishedMessage{MessageID: published.MessageIDs[0]}, nil, topicManagementReceipt(call, published.MessageIDs[0]))
}

// PullMessagesOperation reads available deliveries from a pull subscription.
type PullMessagesOperation struct{ client *Client }

// Definition returns the generated pull contract.
func (PullMessagesOperation) Definition() sdkgo.QueryDefinition { return PullMessagesDefinition }

// PullMessages binds the pull Query to the client.
func (client *Client) PullMessages() PullMessagesOperation { return PullMessagesOperation{client} }

// Invoke requests up to MaxMessages without acknowledging them.
func (operation PullMessagesOperation) Invoke(call sdkgo.Call, input PullMessagesInput) sdkgo.QueryAttempt[PulledMessages] {
	if !validSubscriptionName(input.Subscription) || input.MaxMessages < 1 || input.MaxMessages > 1000 {
		return topicManagementQueryBranch(PullMessagesBranchDefect, PulledMessages{}, sdkgo.FailureValidation, "Pub/Sub pull input is invalid")
	}
	body, _ := json.Marshal(struct {
		MaxMessages int `json:"maxMessages"`
	}{input.MaxMessages})
	response := operation.client.request(call, http.MethodPost, "/v1/"+input.Subscription+":pull", body)
	if failure := topicManagementResponseFailure(response); failure != nil {
		return topicManagementQueryFailure[PulledMessages](*failure, PullMessagesBranchDefect, PullMessagesBranchInvalidResponse)
	}
	if response.status == http.StatusNotFound {
		return topicManagementQueryBranch(PullMessagesBranchNotFound, PulledMessages{}, sdkgo.FailureNotFound, "Pub/Sub subscription was not found")
	}
	if response.status != http.StatusOK {
		return topicManagementQueryStatus[PulledMessages](response, PullMessagesBranchProviderRejected, "pullMessages")
	}
	var pulled PulledMessages
	if json.Unmarshal(response.body, &pulled) != nil {
		return topicManagementQueryBranch(PullMessagesBranchInvalidResponse, PulledMessages{}, sdkgo.FailureProtocol, "Pub/Sub returned invalid pull JSON")
	}
	for _, received := range pulled.ReceivedMessages {
		if received.AckID == "" || received.Message.MessageID == "" || (received.Message.Data == "" && len(received.Message.Attributes) == 0) {
			return topicManagementQueryBranch(PullMessagesBranchInvalidResponse, PulledMessages{}, sdkgo.FailureProtocol, "Pub/Sub returned an incomplete message")
		}
		if received.Message.Data != "" {
			if _, err := base64.StdEncoding.DecodeString(received.Message.Data); err != nil {
				return topicManagementQueryBranch(PullMessagesBranchInvalidResponse, PulledMessages{}, sdkgo.FailureProtocol, "Pub/Sub returned invalid message data")
			}
		}
	}
	return sdkgo.NewQueryBranch(PullMessagesBranchPulled, pulled, nil, sdkgo.Receipt{})
}

// AcknowledgeMessagesOperation confirms pulled deliveries.
type AcknowledgeMessagesOperation struct{ client *Client }

// Definition returns the generated acknowledge contract.
func (AcknowledgeMessagesOperation) Definition() sdkgo.MutationDefinition {
	return AcknowledgeMessagesDefinition
}

// IdempotencyKey identifies the logical acknowledgement.
func (AcknowledgeMessagesOperation) IdempotencyKey(callID sdkgo.CallID, _ AcknowledgeMessagesInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// AcknowledgeMessages binds the acknowledgement Mutation to the client.
func (client *Client) AcknowledgeMessages() AcknowledgeMessagesOperation {
	return AcknowledgeMessagesOperation{client}
}

// Invoke acknowledges all supplied IDs.
func (operation AcknowledgeMessagesOperation) Invoke(call sdkgo.Call, input AcknowledgeMessagesInput) sdkgo.MutationAttempt[AcknowledgedMessages] {
	if !validSubscriptionName(input.Subscription) || len(input.AckIDs) == 0 || len(input.AckIDs) > 1000 {
		return topicManagementMutationBranch(AcknowledgeMessagesBranchDefect, AcknowledgedMessages{}, sdkgo.FailureValidation, "Pub/Sub acknowledgement input is invalid")
	}
	for _, ackID := range input.AckIDs {
		if ackID == "" || len(ackID) > 4096 || strings.ContainsAny(ackID, "\r\n") {
			return topicManagementMutationBranch(AcknowledgeMessagesBranchDefect, AcknowledgedMessages{}, sdkgo.FailureValidation, "Pub/Sub acknowledgement ID is invalid")
		}
	}
	body, _ := json.Marshal(struct {
		AckIDs []string `json:"ackIds"`
	}{input.AckIDs})
	response := operation.client.request(call, http.MethodPost, "/v1/"+input.Subscription+":acknowledge", body)
	if failure := topicManagementResponseFailure(response); failure != nil {
		return topicManagementMutationFailure[AcknowledgedMessages](*failure, AcknowledgeMessagesBranchDefect, AcknowledgeMessagesBranchProviderRejected)
	}
	if response.status == http.StatusNotFound {
		return topicManagementMutationBranch(AcknowledgeMessagesBranchNotFound, AcknowledgedMessages{}, sdkgo.FailureNotFound, "Pub/Sub subscription was not found")
	}
	if response.status != http.StatusOK && response.status != http.StatusNoContent {
		return topicManagementMutationStatus[AcknowledgedMessages](response, AcknowledgeMessagesBranchProviderRejected, "acknowledgeMessages")
	}
	return sdkgo.NewMutationBranch(AcknowledgeMessagesBranchAcknowledged, AcknowledgedMessages{AcknowledgedCount: len(input.AckIDs)}, nil, topicManagementReceipt(call, input.Subscription))
}

func validMessageAttributes(attributes map[string]string) bool {
	if len(attributes) > 100 {
		return false
	}
	for key, value := range attributes {
		if key == "" || len(key) > 256 || len(value) > 1024 || strings.ContainsAny(key+value, "\r\n") {
			return false
		}
	}
	return true
}
