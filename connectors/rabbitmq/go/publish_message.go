// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package rabbitmq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// PublishMessageInput identifies an exchange and supplies one UTF-8 message.
type PublishMessageInput struct {
	// VirtualHost is the exact virtual host, commonly /.
	VirtualHost string `json:"virtualHost"`
	// Exchange is the exchange name; an empty name selects the default exchange.
	Exchange string `json:"exchange"`
	// RoutingKey selects the destination binding or queue for the default exchange.
	RoutingKey string `json:"routingKey"`
	// Payload is the UTF-8 message body.
	Payload string `json:"payload"`
}

// PublishedMessage reports whether the message reached at least one queue.
type PublishedMessage struct {
	// Routed is true when RabbitMQ routed the message to a queue.
	Routed bool `json:"routed"`
}

// PublishMessageOperation publishes through the Management HTTP API.
type PublishMessageOperation struct{ client *Client }

// PublishMessage binds the publish Mutation to this client.
func (client *Client) PublishMessage() PublishMessageOperation {
	return PublishMessageOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (PublishMessageOperation) Definition() sdkgo.MutationDefinition { return PublishMessageDefinition }

// IdempotencyKey records the call identity; RabbitMQ does not deduplicate it.
func (PublishMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ PublishMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke publishes once per permitted send and reports an unconfirmed outcome at the cap.
func (operation PublishMessageOperation) Invoke(call sdkgo.Call, input PublishMessageInput) sdkgo.MutationAttempt[PublishedMessage] {
	if !validQueueIdentity(input.VirtualHost) || len(input.Exchange) > 255 || !utf8.ValidString(input.Exchange) || !validQueueIdentity(input.RoutingKey) || !utf8.ValidString(input.Payload) || len(input.Payload) > 1048576 {
		return publishBranch(PublishMessageBranchDefect, sdkgo.FailureValidation, "RabbitMQ publish input is invalid")
	}
	fence, err := singledispatch.NewFence(call, PublishMessageDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return publishBranch(PublishMessageBranchDefect, sdkgo.FailureLocalDefect, "RabbitMQ dispatch checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind != singledispatch.SendAllowed && decision.Kind != singledispatch.ReadBackThenResendAllowed {
		return publishUncertain(sdkgo.FailureTransport, "RabbitMQ publish outcome could not be confirmed")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.Username) || strings.Contains(credentials.Username, ":") || !providerhttp.IsHeaderSafeCredential(credentials.Password.Reveal()) {
		return publishBranch(PublishMessageBranchDefect, sdkgo.FailureAuthentication, "RabbitMQ credentials are unavailable")
	}
	payload, err := json.Marshal(map[string]any{"properties": map[string]any{}, "routing_key": input.RoutingKey, "payload": input.Payload, "payload_encoding": "string"})
	if err != nil {
		return publishBranch(PublishMessageBranchDefect, sdkgo.FailureLocalDefect, "RabbitMQ publish payload could not be encoded")
	}
	endpoint := operation.client.managementURL + "/api/exchanges/" + url.PathEscape(input.VirtualHost) + "/" + url.PathEscape(input.Exchange) + "/publish"
	request, err := http.NewRequestWithContext(call.Context, http.MethodPut, endpoint, bytes.NewReader(payload))
	if err != nil {
		return publishBranch(PublishMessageBranchDefect, sdkgo.FailureLocalDefect, "RabbitMQ publish request could not be built")
	}
	request.SetBasicAuth(credentials.Username, credentials.Password.Reveal())
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return publishBranch(PublishMessageBranchDefect, sdkgo.FailureLocalDefect, "RabbitMQ publish request is invalid")
	}
	dispatch := publishDispatch{client: operation.client, request: request}
	if err := fence.Dispatch(dispatch.send); err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return publishRetry(sdkgo.FailureTransport, "RabbitMQ message was not sent", 0)
		}
		if !dispatch.didGetConnection.Load() && fence.RecordCanResend() == nil {
			return publishRetry(sdkgo.FailureTransport, "RabbitMQ was unreachable before sending", 0)
		}
		return publishRetry(sdkgo.FailureTransport, "RabbitMQ publish outcome is unknown", 0)
	}
	defer func() { _ = dispatch.response.Body.Close() }()
	if dispatch.response.StatusCode == http.StatusTooManyRequests || dispatch.response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return publishRetry(sdkgo.FailureAvailability, "RabbitMQ retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if dispatch.response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return publishRetry(kind, "RabbitMQ did not publish the message", providerhttp.ParseRetryAfter(dispatch.response.Header.Get("Retry-After"), time.Now()))
	}
	if dispatch.response.StatusCode == http.StatusRequestTimeout || dispatch.response.StatusCode >= 500 {
		return publishRetry(sdkgo.FailureAvailability, "RabbitMQ publish outcome is unknown", 0)
	}
	if dispatch.response.StatusCode != http.StatusOK {
		return publishBranch(PublishMessageBranchProviderRejected, sdkgo.FailureProviderRejection, rejectedStatus(dispatch.response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(dispatch.response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return publishUncertain(sdkgo.FailureProtocol, "RabbitMQ publish response could not be read")
	}
	var response struct {
		Routed *bool `json:"routed"`
	}
	if json.Unmarshal(body, &response) != nil || response.Routed == nil {
		return publishUncertain(sdkgo.FailureProtocol, "RabbitMQ publish response is invalid")
	}
	if !*response.Routed {
		return sdkgo.NewMutationBranch(PublishMessageBranchUnroutable, PublishedMessage{Routed: false}, nil, sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(PublishMessageBranchPublished, PublishedMessage{Routed: true}, nil, sdkgo.Receipt{})
}

type publishDispatch struct {
	client           *Client
	request          *http.Request
	response         *http.Response
	didGetConnection atomic.Bool
}

func (dispatch *publishDispatch) send(requestContext context.Context) error {
	trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { dispatch.didGetConnection.Store(true) }}
	request := dispatch.request.WithContext(httptrace.WithClientTrace(requestContext, trace))
	response, err := dispatch.client.httpClient.Do(request)
	dispatch.response = response
	return err
}

func publishFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "rabbitmq", Operation: "publishMessage", Message: message}
}

func publishBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[PublishedMessage] {
	failure := publishFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, PublishedMessage{}, &failure, sdkgo.Receipt{})
}

func publishRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[PublishedMessage] {
	return sdkgo.NewMutationRetry[PublishedMessage](publishFailure(kind, message), retryAfter)
}

func publishUncertain(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[PublishedMessage] {
	return sdkgo.NewMutationUncertain(PublishedMessage{}, publishFailure(kind, message), sdkgo.Receipt{})
}
