// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package discord

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const (
	// MaximumMessageCharacters is Discord's limit on message content length in Unicode characters.
	MaximumMessageCharacters = 2000
	// nonceLength is the 25-character maximum Discord accepts for a string nonce.
	nonceLength = 25
)

// CreateMessageInput sends one text message to a channel as the bot.
//
// Every mention is disabled, so text such as @everyone or <@id> renders but
// notifies nobody.
type CreateMessageInput struct {
	// ChannelID is the channel's snowflake ID, 17 to 20 digits.
	ChannelID string `json:"channelId"`
	// Content is the message text, 1 to 2000 characters and not only whitespace.
	Content string `json:"content"`
	// ReplyToMessageID makes the message a reply to this message in the same channel; blank sends a plain message.
	ReplyToMessageID string `json:"replyToMessageId,omitempty"`
}

// CreateMessageOperation implements the createMessage connector Mutation.
type CreateMessageOperation struct{ client *Client }

type createMessageRequestBody struct {
	Content          string                   `json:"content"`
	Nonce            string                   `json:"nonce"`
	EnforceNonce     bool                     `json:"enforce_nonce"`
	AllowedMentions  allowedMentions          `json:"allowed_mentions"`
	MessageReference *messageReferenceRequest `json:"message_reference,omitempty"`
}

type allowedMentions struct {
	Parse []string `json:"parse"`
}

type messageReferenceRequest struct {
	MessageID       string `json:"message_id"`
	FailIfNotExists bool   `json:"fail_if_not_exists"`
}

var createMessageBranches = operationBranches{
	notFound: CreateMessageBranchNotFound, rejected: CreateMessageBranchProviderRejected,
	invalidResponse: CreateMessageBranchInvalidResponse, defect: CreateMessageBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (CreateMessageOperation) Definition() sdkgo.MutationDefinition { return CreateMessageDefinition }

// IdempotencyKey derives the call's key from the stable connector call ID and input.
func (CreateMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one POST with a nonce derived from the idempotency key and
// enforce_nonce under a single-dispatch fence, then classifies the attempt.
// An unknown outcome resends with the same nonce while possible sends stay
// below call.MaxSends() and selects uncertain at the cap. A definite refusal
// of the first send selects its branch and is never resent. A 2xx is never
// resent: an unreadable answer selects uncertain, and an answer whose message
// ID is valid is checkpointed and read back by that ID.
func (operation CreateMessageOperation) Invoke(call sdkgo.Call, input CreateMessageInput) sdkgo.MutationAttempt[Message] {
	requestBody, err := buildCreateMessageRequestBody(input, call.IdempotencyKey)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateMessageBranchDefect, Message{}, providerFailurePointer("createMessage", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateMessageBranchDefect, Message{}, providerFailurePointer("createMessage", sdkgo.FailureLocalDefect, "createMessage request could not be encoded"), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreateMessageDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CreateMessageBranchDefect, Message{}, providerFailurePointer("createMessage", sdkgo.FailureLocalDefect, "Discord dispatch fence could not be constructed"), sdkgo.Receipt{})
	}
	receipt := operation.client.receipt(call, input.ChannelID)
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		// An unreadable checkpoint is an unknown outcome; the retry budget bounds these attempts.
		return retryCreateMessage(sdkgo.FailureAvailability, "Discord dispatch checkpoint is unreadable; no message was sent in this attempt", 0)
	}
	// A saved message ID proves Discord accepted an earlier send, so it is read back at any send count and never resent.
	if decision.Kind != singledispatch.SendAllowed && decision.Marker.ProviderID != "" {
		return operation.readBackSavedMessage(call, input.ChannelID, decision.Marker.ProviderID)
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return uncertainCreateMessage(sdkgo.FailureTransport, "an earlier attempt may have created the Discord message, and the send cap is reached", receipt)
	}
	// Without a saved message ID, the resend with the same enforced nonce is Discord's only read-back.
	// This attempt's send would be the last one the cap allows when no further send remains.
	canResendUnknownOutcome := decision.MaximumPriorSends+1 < call.MaxSends()
	// An earlier send may have created the message, so a refusal of this resend cannot prove nothing changed.
	hasPossibleEarlierSend := decision.MaximumPriorSends > 0
	credentials, outcome := operation.client.resolveCredentials(call, "createMessage")
	if outcome != nil {
		return localCreateMessageFailure(fence, hasPossibleEarlierSend, *outcome, receipt)
	}
	httpRequest, err := operation.client.newHTTPRequest(call.Context, credentials, providerRequest{
		method: http.MethodPost, path: "/channels/" + input.ChannelID + "/messages", body: encoded,
	})
	if err == nil {
		err = singledispatch.PrepareHTTPWrite(httpRequest)
	}
	if err != nil {
		failure := operation.client.outcome(outcomeDefect, "createMessage", sdkgo.FailureLocalDefect, errRequestNotBuilt.Error(), sdkgo.Receipt{})
		return localCreateMessageFailure(fence, hasPossibleEarlierSend, *failure, receipt)
	}
	observation := &dispatchObservation{}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		var sendErr error
		response, sendErr = operation.client.do(httpRequest.WithContext(httptrace.WithClientTrace(requestContext, observation.clientTrace())))
		return sendErr
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return retryCreateMessage(sdkgo.FailureAvailability, "Discord dispatch was not started", 0)
	}
	if response.statusCode == 0 {
		if err != nil && observation.isProvablyUndispatched() {
			// A lost canResend checkpoint leaves the claim, which the next attempt counts as a send.
			_ = fence.RecordCanResend()
			return retryCreateMessage(sdkgo.FailureTransport, "Discord could not be reached before sending", 0)
		}
		return resendOrSelectUncertain(canResendUnknownOutcome, sdkgo.FailureTransport, "Discord message outcome is unknown", 0, receipt)
	}
	isSuccessStatus := response.statusCode >= 200 && response.statusCode < 300
	if !isSuccessStatus {
		return classifyCreateMessageRefusal(fence, hasPossibleEarlierSend, canResendUnknownOutcome, input.ChannelID, response, err, operation.client.now(), receipt)
	}
	// A 2xx proves Discord created the message, and nonce deduplication is unverified, so nothing below resends.
	if err != nil {
		return uncertainCreateMessage(sdkgo.FailureTransport, "Discord accepted the message, but its response could not be read", receipt)
	}
	var decoded providerMessage
	message, err := Message{}, decodeResponse(response.body, &decoded)
	if err == nil {
		message, err = convertProviderMessage(decoded, input.ChannelID)
	}
	if err != nil {
		return operation.readBackAcceptedMessage(call, fence, credentials, input.ChannelID, response.body, err, receipt)
	}
	return sdkgo.NewMutationBranch(CreateMessageBranchCreated, message, nil, operation.client.receipt(call, message.ID))
}

// readBackAcceptedMessage confirms a 2xx whose body is not a valid message. A valid
// top-level message ID is checkpointed and read back; without one the outcome is uncertain.
func (operation CreateMessageOperation) readBackAcceptedMessage(call sdkgo.Call, fence *singledispatch.Fence, credentials Credentials, channelID string, body []byte, decodeErr error, receipt sdkgo.Receipt) sdkgo.MutationAttempt[Message] {
	// A map matches the "id" key exactly, as the Python and TypeScript ports do.
	var fields map[string]any
	messageID := ""
	if json.Unmarshal(body, &fields) == nil {
		messageID, _ = fields["id"].(string)
	}
	if !snowflakePattern.MatchString(messageID) {
		return uncertainCreateMessage(sdkgo.FailureProtocol, "Discord accepted the message, but its response carries no message ID: "+decodeErr.Error(), receipt)
	}
	isMessageIDSaved := fence.RecordProviderID(messageID) == nil
	return operation.readBackMessage(call, credentials, channelID, messageID, isMessageIDSaved)
}

// readBackSavedMessage reads back the message ID an earlier attempt checkpointed.
func (operation CreateMessageOperation) readBackSavedMessage(call sdkgo.Call, channelID string, messageID string) sdkgo.MutationAttempt[Message] {
	receipt := operation.client.receipt(call, messageID)
	if !snowflakePattern.MatchString(messageID) {
		return uncertainCreateMessage(sdkgo.FailureProtocol, "an earlier attempt created the Discord message, but its checkpointed message ID is invalid", receipt)
	}
	credentials, outcome := operation.client.resolveCredentials(call, "createMessage")
	if outcome != nil {
		if outcome.disposition == outcomeRetry {
			// The checkpoint keeps the saved message ID, so the next attempt reads it back again.
			return sdkgo.NewMutationRetry[Message](outcome.failure, outcome.retryAfter)
		}
		return uncertainCreateMessage(outcome.failure.Kind, "an earlier attempt created the Discord message, but it cannot be read back: "+outcome.failure.Message, receipt)
	}
	return operation.readBackMessage(call, credentials, channelID, messageID, true)
}

// readBackMessage reads an accepted message by ID and selects created when Discord returns it.
// Only a transport or read failure, an oversized body, 408, 429, or 5xx retries, and only when the
// ID is checkpointed for the next attempt. Every other answer, such as a complete but unusable 2xx,
// 404, or a missing Read Message History permission, selects uncertain.
func (operation CreateMessageOperation) readBackMessage(call sdkgo.Call, credentials Credentials, channelID string, messageID string, isMessageIDSaved bool) sdkgo.MutationAttempt[Message] {
	receipt := operation.client.receipt(call, messageID)
	response, err := operation.client.send(call, credentials, providerRequest{method: http.MethodGet, path: "/channels/" + channelID + "/messages/" + messageID})
	failure := providerFailure("createMessage", sdkgo.FailureTransport, "Discord accepted the message, but reading it back did not complete")
	var retryAfter time.Duration
	switch {
	case errors.Is(err, errRequestNotBuilt):
		return uncertainCreateMessage(sdkgo.FailureLocalDefect, "Discord accepted the message, but its read-back request could not be built", receipt)
	case response.statusCode >= 200 && response.statusCode < 300 && err == nil:
		var decoded providerMessage
		message, convertErr := Message{}, decodeResponse(response.body, &decoded)
		if convertErr == nil {
			message, convertErr = convertProviderMessage(decoded, channelID)
		}
		if convertErr == nil && message.ID == messageID {
			return sdkgo.NewMutationBranch(CreateMessageBranchCreated, message, nil, receipt)
		}
		// A complete answer that is unusable will not change on a retry, and a failed Flow invites a duplicate.
		return uncertainCreateMessage(sdkgo.FailureProtocol, "Discord accepted the message, but its read-back answer is invalid", receipt)
	case response.statusCode != 0 && (response.statusCode < 200 || response.statusCode >= 300):
		classified := classifyErrorResponse("createMessage", response, operation.client.now())
		failure, retryAfter = classified.failure, classified.retryAfter
		failure.Message = "Discord accepted the message, but reading it back failed: " + failure.Message
		if err == nil && !response.isErrorBodyOversized && !isTransientReadBackStatus(response.statusCode) {
			return sdkgo.NewMutationUncertain(Message{}, failure, receipt)
		}
	}
	if isMessageIDSaved {
		return sdkgo.NewMutationRetry[Message](failure, retryAfter)
	}
	return sdkgo.NewMutationUncertain(Message{}, failure, receipt)
}

// classifyCreateMessageRefusal classifies a non-2xx answer: 429 and 503 prove no processing, a
// readable allowlisted refusal selects its branch, and every other answer is an unknown outcome.
func classifyCreateMessageRefusal(fence *singledispatch.Fence, hasPossibleEarlierSend bool, canResendUnknownOutcome bool, channelID string, response providerResponse, readErr error, now time.Time, receipt sdkgo.Receipt) sdkgo.MutationAttempt[Message] {
	classified := classifyErrorResponse("createMessage", response, now)
	if response.statusCode == http.StatusTooManyRequests || response.statusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return retryCreateMessage(sdkgo.FailureAvailability, "Discord retry checkpoint could not be recorded", 0)
		}
		return sdkgo.NewMutationRetry[Message](classified.failure, classified.retryAfter)
	}
	isBodyReadable := readErr == nil && !response.isErrorBodyOversized
	isDefiniteRefusal := isBodyReadable && isRefusalStatus(response.statusCode) && !isAcceptedMessageBody(response.body, channelID) &&
		(classified.disposition == outcomeNotFound || classified.disposition == outcomeRejected)
	if !isDefiniteRefusal {
		// An unreadable, contradictory, or unlisted answer cannot prove that Discord created nothing.
		return resendOrSelectUncertain(canResendUnknownOutcome, classified.failure.Kind, classified.failure.Message, classified.retryAfter, receipt)
	}
	if hasPossibleEarlierSend {
		return uncertainCreateMessage(classified.failure.Kind, "an earlier attempt may have created the Discord message: "+classified.failure.Message, receipt)
	}
	// The branch is terminal, so a lost canResend checkpoint cannot cause a resend.
	_ = fence.RecordCanResend()
	return sdkgo.NewMutationBranch(createMessageBranches.branchFor(classified.disposition), Message{}, &classified.failure, receipt)
}

// localCreateMessageFailure handles a failure before dispatch: a transient one retries, recording
// notSent only when no earlier send may have created the message; a terminal one selects its
// branch before any possible send and uncertain after one.
func localCreateMessageFailure(fence *singledispatch.Fence, hasPossibleEarlierSend bool, outcome providerOutcome, receipt sdkgo.Receipt) sdkgo.MutationAttempt[Message] {
	if outcome.disposition == outcomeRetry {
		if !hasPossibleEarlierSend {
			// Without notSent the next attempt counts this attempt as a possible send, which only lowers the cap.
			_ = fence.RecordNotSent()
		}
		// After a possible send, notSent would hide that send, so nothing is recorded.
		return sdkgo.NewMutationRetry[Message](outcome.failure, outcome.retryAfter)
	}
	if hasPossibleEarlierSend {
		return uncertainCreateMessage(outcome.failure.Kind, "an earlier attempt may have created the Discord message: "+outcome.failure.Message, receipt)
	}
	// The branch is terminal, so a lost notSent checkpoint cannot cause a resend.
	_ = fence.RecordNotSent()
	return sdkgo.NewMutationBranch(createMessageBranches.branchFor(outcome.disposition), Message{}, &outcome.failure, sdkgo.Receipt{})
}

// resendOrSelectUncertain retries a possibly created message while the send cap allows another send.
func resendOrSelectUncertain(canResendUnknownOutcome bool, kind sdkgo.FailureKind, message string, retryAfter time.Duration, receipt sdkgo.Receipt) sdkgo.MutationAttempt[Message] {
	if canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt counts this send.
		return retryCreateMessage(kind, message, retryAfter)
	}
	return uncertainCreateMessage(kind, message, receipt)
}

// isTransientReadBackStatus reports a read-back status that a later attempt may answer differently.
func isTransientReadBackStatus(statusCode int) bool {
	return statusCode == http.StatusRequestTimeout || statusCode == http.StatusTooManyRequests || statusCode >= 500
}

// isRefusalStatus allowlists request refusals; 408, 409, 429, and unlisted 4xx keep the claim.
func isRefusalStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return true
	}
	return false
}

// isAcceptedMessageBody reports an error body that nevertheless describes a message in the channel.
func isAcceptedMessageBody(body []byte, channelID string) bool {
	var decoded providerMessage
	if decodeResponse(body, &decoded) != nil {
		return false
	}
	_, err := convertProviderMessage(decoded, channelID)
	return err == nil
}

func retryCreateMessage(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[Message] {
	return sdkgo.NewMutationRetry[Message](providerFailure("createMessage", kind, message), retryAfter)
}

func uncertainCreateMessage(kind sdkgo.FailureKind, message string, receipt sdkgo.Receipt) sdkgo.MutationAttempt[Message] {
	return sdkgo.NewMutationUncertain(Message{}, providerFailure("createMessage", kind, message), receipt)
}

func buildCreateMessageRequestBody(input CreateMessageInput, idempotencyKey sdkgo.IdempotencyKey) (createMessageRequestBody, error) {
	if err := validateSnowflake("channelId", input.ChannelID); err != nil {
		return createMessageRequestBody{}, err
	}
	if strings.TrimSpace(input.Content) == "" {
		return createMessageRequestBody{}, errors.New("content must not be blank")
	}
	if !utf8.ValidString(input.Content) || utf8.RuneCountInString(input.Content) > MaximumMessageCharacters {
		return createMessageRequestBody{}, errors.New("content must be valid UTF-8 of at most 2000 characters")
	}
	if idempotencyKey == "" {
		return createMessageRequestBody{}, errors.New("createMessage requires an idempotency key")
	}
	body := createMessageRequestBody{
		Content: input.Content, Nonce: messageNonce(idempotencyKey), EnforceNonce: true,
		AllowedMentions: allowedMentions{Parse: []string{}},
	}
	if input.ReplyToMessageID != "" {
		if err := validateSnowflake("replyToMessageId", input.ReplyToMessageID); err != nil {
			return createMessageRequestBody{}, err
		}
		body.MessageReference = &messageReferenceRequest{MessageID: input.ReplyToMessageID, FailIfNotExists: true}
	}
	return body, nil
}

// messageNonce shortens the idempotency key to Discord's 25-character nonce
// limit with SHA-256 and URL-safe base64, about 150 bits of the digest.
func messageNonce(idempotencyKey sdkgo.IdempotencyKey) string {
	digest := sha256.Sum256([]byte(idempotencyKey))
	return base64.RawURLEncoding.EncodeToString(digest[:])[:nonceLength]
}
