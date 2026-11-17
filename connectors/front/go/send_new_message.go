// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// SendNewMessageInput sends a message that begins a new Front conversation.
type SendNewMessageInput struct {
	// ChannelAddress is the sending channel address from Front Settings > Channels, such as support@example.com.
	// Front resolves it through its alt:address resource alias, so no channel ID lookup is needed.
	ChannelAddress string `json:"channelAddress"`
	// To lists the recipient handles for the channel; at least one is required.
	To []string `json:"to"`
	// Text is the plain text to send, at most MaxReplyTextBytes; email receives an HTML equivalent too.
	Text string `json:"text"`
	// Subject is the email subject; blank lets Front choose the channel default.
	Subject string `json:"subject,omitempty"`
	// AuthorID is the optional teammate tea_ identifier on whose behalf Front sends.
	AuthorID string `json:"authorId,omitempty"`
}

// SentMessage is Front's accepted message UID, which can be read through GET /messages/alt:uid:{uid}.
type SentMessage struct {
	// MessageUID is Front's stable accepted-message UID.
	MessageUID string `json:"messageUid"`
}

// SendNewMessageOperation creates a message from a Front channel.
type SendNewMessageOperation struct{ client *Client }

type newMessageWire struct {
	To       []string `json:"to"`
	Body     string   `json:"body"`
	Text     string   `json:"text"`
	Subject  string   `json:"subject,omitempty"`
	AuthorID string   `json:"author_id,omitempty"`
}

// SendNewMessage binds the mutation to this client.
func (client *Client) SendNewMessage() SendNewMessageOperation {
	return SendNewMessageOperation{client}
}

// Definition returns the manifest-generated mutation contract.
func (SendNewMessageOperation) Definition() sdkgo.MutationDefinition { return SendNewMessageDefinition }

// IdempotencyKey identifies the fenced call. Front does not receive this key.
func (SendNewMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ SendNewMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends within the limited repeat budget and reads back accepted message UIDs on later attempts.
func (operation SendNewMessageOperation) Invoke(call sdkgo.Call, input SendNewMessageInput) sdkgo.MutationAttempt[SentMessage] {
	operationID := SendNewMessageDefinition.Operation.OperationID
	if !validNewMessageInput(input) {
		return sdkgo.NewMutationBranch(SendNewMessageBranchDefect, SentMessage{}, frontFailurePointer(sdkgo.FailureValidation, operationID, "channelAddress, to, text, or authorId is invalid"), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, SendNewMessageDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(SendNewMessageBranchDefect, SentMessage{}, frontFailurePointer(sdkgo.FailureLocalDefect, operationID, "dispatch fence unavailable"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		return operation.uncertain(call, "earlier message send may have succeeded", "")
	case singledispatch.ReadBackRequired:
		return operation.readBack(call, decision.Marker.ProviderID)
	case singledispatch.ReadBackThenResendAllowed:
		if decision.Marker.ProviderID != "" {
			return operation.readBack(call, decision.Marker.ProviderID)
		}
	}
	credentials, failure, credentialError := operation.client.resolveCredentialsWithCause(call, operationID)
	if failure != nil {
		if decision.MaximumPriorSends > 0 {
			return operation.uncertain(call, "earlier message send may have succeeded; reconnect before checking Front", "")
		}
		if fence.RecordNotSent() != nil {
			return sdkgo.NewMutationRetry[SentMessage](frontFailure(sdkgo.FailureAvailability, operationID, "credential checkpoint unavailable"), 0)
		}
		if isTerminalCredentialFailure(credentialError) {
			return sdkgo.NewMutationBranch(SendNewMessageBranchDefect, SentMessage{}, failure, sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[SentMessage](*failure, 0)
	}
	request := frontRequest{method: http.MethodPost, path: "/channels/alt:address:" + url.PathEscape(input.ChannelAddress) + "/messages", payload: newMessageWire{To: input.To, Body: BuildReplyHTML(input.Text), Text: input.Text, Subject: input.Subject, AuthorID: input.AuthorID}}
	var result frontExchange
	err = fence.Dispatch(func(requestContext context.Context) error {
		result = operation.client.exchangeFencedWrite(requestContext, credentials, operationID, request)
		return nil
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[SentMessage](frontFailure(sdkgo.FailureAvailability, operationID, "message was not sent before checkpoint"), 0)
	}
	if err != nil {
		return operation.uncertain(call, "message dispatch outcome is unknown", "")
	}
	if result.outcome == exchangeSucceeded {
		uid, _ := decodeReplyIdentity(result.response.body, false)
		if uid == "" {
			return operation.uncertain(call, "Front accepted the message without a usable UID", "")
		}
		_ = fence.RecordProviderID(uid)
		return sdkgo.NewMutationBranch(SendNewMessageBranchSent, SentMessage{MessageUID: uid}, nil, operation.client.receipt(call, uid))
	}
	if result.response.statusCode >= 200 && result.response.statusCode < 300 {
		return operation.uncertain(call, "Front accepted the message without a trustworthy response", "")
	}
	switch result.outcome {
	case exchangeNotSent, exchangeRateLimited:
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[SentMessage](frontFailure(sdkgo.FailureAvailability, operationID, "resend checkpoint unavailable"), 0)
		}
		return sdkgo.NewMutationRetry[SentMessage](result.failure, result.retryAfter)
	case exchangeUnconfirmed:
		if result.response.statusCode == http.StatusServiceUnavailable {
			if fence.RecordCanResend() != nil {
				return sdkgo.NewMutationRetry[SentMessage](frontFailure(sdkgo.FailureAvailability, operationID, "resend checkpoint unavailable"), 0)
			}
			return sdkgo.NewMutationRetry[SentMessage](result.failure, result.retryAfter)
		}
		if decision.MaximumPriorSends+1 < call.MaxSends() {
			return sdkgo.NewMutationRetry[SentMessage](result.failure, result.retryAfter)
		}
		return operation.uncertain(call, "message outcome is unknown after the send cap", "")
	case exchangeDefect:
		return sdkgo.NewMutationBranch(SendNewMessageBranchDefect, SentMessage{}, &result.failure, sdkgo.Receipt{})
	case exchangeRejected, exchangeNotFound:
		if decision.MaximumPriorSends > 0 {
			return operation.uncertain(call, "an earlier message send may have succeeded", "")
		}
		return sdkgo.NewMutationBranch(SendNewMessageBranchProviderRejected, SentMessage{}, &result.failure, operation.client.receipt(call, ""))
	default:
		return operation.uncertain(call, "message outcome is unknown", "")
	}
}

func (operation SendNewMessageOperation) readBack(call sdkgo.Call, uid string) sdkgo.MutationAttempt[SentMessage] {
	operationID := SendNewMessageDefinition.Operation.OperationID
	if !messageUIDPattern.MatchString(uid) {
		return operation.uncertain(call, "prior message has no valid UID for read-back", uid)
	}
	credentials, failure, credentialError := operation.client.resolveCredentialsWithCause(call, operationID)
	if failure != nil {
		if isTerminalCredentialFailure(credentialError) {
			return operation.uncertain(call, "Front credentials require reconnection before message read-back", uid)
		}
		return sdkgo.NewMutationRetry[SentMessage](*failure, 0)
	}
	result := operation.client.exchange(call.Context, credentials, operationID, frontRequest{method: http.MethodGet, path: "/messages/alt:uid:" + uid})
	if result.outcome == exchangeSucceeded {
		return sdkgo.NewMutationBranch(SendNewMessageBranchSent, SentMessage{MessageUID: uid}, nil, operation.client.receipt(call, uid))
	}
	if result.isRetryable() || result.outcome == exchangeNotFound {
		return sdkgo.NewMutationRetry[SentMessage](result.failure, result.retryAfter)
	}
	return operation.uncertain(call, "Front could not confirm the accepted message", uid)
}

func (operation SendNewMessageOperation) uncertain(call sdkgo.Call, message, uid string) sdkgo.MutationAttempt[SentMessage] {
	return sdkgo.NewMutationUncertain(SentMessage{MessageUID: uid}, frontFailure(sdkgo.FailureAvailability, SendNewMessageDefinition.Operation.OperationID, message), operation.client.receipt(call, uid))
}

func validNewMessageInput(input SendNewMessageInput) bool {
	if input.ChannelAddress == "" || len(input.ChannelAddress) > 254 || strings.ContainsAny(input.ChannelAddress, "/?#\r\n\t ") || len(input.To) == 0 || len(input.To) > 100 || strings.TrimSpace(input.Text) == "" || len(input.Text) > MaxReplyTextBytes || len(input.Subject) > 998 || (input.AuthorID != "" && !teammateIDPattern.MatchString(input.AuthorID)) {
		return false
	}
	for _, recipient := range input.To {
		if recipient == "" || len(recipient) > 254 || strings.ContainsAny(recipient, "\r\n") {
			return false
		}
	}
	return true
}
