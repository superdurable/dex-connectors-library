// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sns

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"net/url"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// PublishMessageInput sends a plain UTF-8 message to a regional SNS topic.
type PublishMessageInput struct {
	// TopicARN is the complete ARN from SNS Console > Topics > selected topic.
	TopicARN string `json:"topicARN"`
	// Message is the UTF-8 message body, at most 256 KiB with the topic's default size limit.
	Message string `json:"message"`
	// Subject is optional and appears in email notifications; at most 100 characters.
	Subject string `json:"subject"`
}

// PublishedMessage identifies a message accepted by SNS; delivery is asynchronous.
type PublishedMessage struct {
	// MessageID is the SNS-assigned identifier.
	MessageID string `json:"messageID"`
}

// PublishMessageOperation implements a bounded message publish.
type PublishMessageOperation struct{ client *Client }

// PublishMessage binds the Mutation to this client.
func (client *Client) PublishMessage() PublishMessageOperation {
	return PublishMessageOperation{client}
}

// Definition returns the generated Mutation contract.
func (PublishMessageOperation) Definition() sdkgo.MutationDefinition { return PublishMessageDefinition }

// IdempotencyKey identifies this Step; SNS does not deduplicate by it.
func (PublishMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ PublishMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke fences each possible send and returns uncertain after the send cap.
func (operation PublishMessageOperation) Invoke(call sdkgo.Call, input PublishMessageInput) sdkgo.MutationAttempt[PublishedMessage] {
	if !topicARNPattern.MatchString(input.TopicARN) || input.Message == "" || len(input.Message) > 256*1024 || !utf8.ValidString(input.Message) ||
		utf8.RuneCountInString(input.Subject) > 100 || !utf8.ValidString(input.Subject) {
		return publishBranch(PublishMessageBranchDefect, sdkgo.FailureValidation, "SNS topic, message, or subject is invalid")
	}
	fence, err := singledispatch.NewFence(call, PublishMessageDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return publishBranch(PublishMessageBranchDefect, sdkgo.FailureLocalDefect, "SNS publish fence could not be initialized")
	}
	switch fence.Decide().Kind {
	case singledispatch.FailClosed, singledispatch.ReadBackRequired:
		return sdkgo.NewMutationUncertain(PublishedMessage{}, topicOperationFailure("publishMessage", sdkgo.FailureTransport, "SNS publish outcome could not be confirmed"), sdkgo.Receipt{})
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil && !errors.Is(checkpointErr, singledispatch.ErrDispatchNotAllowed) {
			return sdkgo.NewMutationUncertain(PublishedMessage{}, topicOperationFailure("publishMessage", sdkgo.FailureTransport, "SNS unsent checkpoint could not be recorded"), sdkgo.Receipt{})
		}
		return publishBranch(PublishMessageBranchDefect, sdkgo.FailureAuthentication, "SNS credentials are unavailable or invalid")
	}
	query := url.Values{"Action": {"Publish"}, "Version": {"2010-03-31"}, "TopicArn": {input.TopicARN}, "Message": {input.Message}}
	if input.Subject != "" {
		query.Set("Subject", input.Subject)
	}
	send := &snsPublishSend{client: operation.client, call: call, query: query}
	if fence.Dispatch(send.dispatch) != nil {
		return sdkgo.NewMutationUncertain(PublishedMessage{}, topicOperationFailure("publishMessage", sdkgo.FailureTransport, "SNS publish dispatch could not be confirmed"), sdkgo.Receipt{})
	}
	response := send.response
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationUncertain(PublishedMessage{}, topicOperationFailure("publishMessage", sdkgo.FailureTransport, "SNS resend checkpoint could not be recorded"), sdkgo.Receipt{})
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[PublishedMessage](topicOperationFailure("publishMessage", kind, "SNS asked the connector to retry publishing"), response.retryAfter)
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[PublishedMessage](topicOperationFailure("publishMessage", sdkgo.FailureTransport, "SNS publish outcome is unknown"), 0)
	}
	if response.status != http.StatusOK {
		return publishBranch(PublishMessageBranchProviderRejected, sdkgo.FailureProviderRejection, "SNS rejected the message")
	}
	var wire struct {
		XMLName xml.Name `xml:"PublishResponse"`
		Result  struct {
			MessageID string `xml:"MessageId"`
		} `xml:"PublishResult"`
	}
	if xml.Unmarshal(response.body, &wire) != nil || wire.XMLName.Local != "PublishResponse" || !isSNSNamespace(wire.XMLName.Space) || wire.Result.MessageID == "" || len(wire.Result.MessageID) > 512 {
		return sdkgo.NewMutationUncertain(PublishedMessage{}, topicOperationFailure("publishMessage", sdkgo.FailureProtocol, "SNS accepted the publish without a usable message ID"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(PublishMessageBranchPublished, PublishedMessage{MessageID: wire.Result.MessageID}, nil,
		sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "amazon-sns", ProviderObjectID: wire.Result.MessageID})
}

type snsPublishSend struct {
	client   *Client
	call     sdkgo.Call
	query    url.Values
	response providerResponse
}

func (send *snsPublishSend) dispatch(requestContext context.Context) error {
	send.response = send.client.requestWithContext(send.call, requestContext, send.query)
	return nil
}

func publishBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[PublishedMessage] {
	failure := topicOperationFailure("publishMessage", kind, message)
	return sdkgo.NewMutationBranch(branch, PublishedMessage{}, &failure, sdkgo.Receipt{})
}
