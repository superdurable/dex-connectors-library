// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// SendMessageInput sends plain text to one bot-accessible chat.
type SendMessageInput struct {
	// ChatID is a decimal chat ID or public @username.
	ChatID string `json:"chatId"`
	// Text is 1 through 4096 UTF-8 characters; Telegram may further reject invalid entities.
	Text string `json:"text"`
}

// SentMessage identifies a Telegram message created by sendMessage.
type SentMessage struct {
	// ChatID is the numeric destination chat ID returned by Telegram.
	ChatID int64 `json:"chatId"`
	// MessageID identifies the message within its chat.
	MessageID int64 `json:"messageId"`
	// Date is the provider's Unix creation time in seconds.
	Date int64 `json:"date"`
}

// SendMessageOperation sends one text message with a durable single-dispatch fence.
type SendMessageOperation struct{ client *Client }

// SendMessage returns the typed message Mutation bound to this client.
func (client *Client) SendMessage() SendMessageOperation { return SendMessageOperation{client} }

// Definition returns the manifest-generated message-send contract.
func (SendMessageOperation) Definition() sdkgo.MutationDefinition { return SendMessageDefinition }

// IdempotencyKey records stable call identity; Telegram does not accept it for deduplication.
func (SendMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ SendMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends under a single-dispatch fence. Telegram has no send idempotency key or message
// read-back, so an unknown outcome resends while possible sends stay below call.MaxSends() and
// selects uncertain at the cap. A definite refusal is never resent.
func (operation SendMessageOperation) Invoke(call sdkgo.Call, input SendMessageInput) sdkgo.MutationAttempt[SentMessage] {
	if !validChatID(input.ChatID) || input.Text == "" || utf8.RuneCountInString(input.Text) > 4096 || !utf8.ValidString(input.Text) {
		return failedSend(SendMessageBranchDefect, sdkgo.FailureValidation, "chatId or text is invalid")
	}
	fence, err := singledispatch.NewFence(call, SendMessageDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return failedSend(SendMessageBranchDefect, sdkgo.FailureLocalDefect, "Telegram dispatch fence is unavailable")
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		return retrySend(sdkgo.FailureAvailability, "Telegram dispatch checkpoint is unreadable; no message was sent in this attempt", 0)
	case singledispatch.ReadBackRequired:
		return uncertainSend(sdkgo.FailureTransport, "an earlier attempt may have sent the Telegram message, and the send cap is reached")
	case singledispatch.ReadBackThenResendAllowed:
		// A saved message ID proves Telegram created the message, so it is never resent.
		if decision.Marker.ProviderID != "" {
			return uncertainSend(sdkgo.FailureTransport, "an earlier attempt sent the Telegram message, but its confirmation was lost")
		}
	}
	// This attempt's send would be the last one the cap allows when no further send remains.
	canResendUnknownOutcome := decision.MaximumPriorSends+1 < call.MaxSends()
	// An earlier send may have created the message, so a refusal of this resend cannot prove nothing changed.
	hasPossibleEarlierSend := decision.MaximumPriorSends > 0
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil {
		if isTerminalCredentialError(err) {
			return localSendFailure(fence, hasPossibleEarlierSend, true, sdkgo.FailureAuthentication, "bot token is unavailable")
		}
		return localSendFailure(fence, hasPossibleEarlierSend, false, sdkgo.FailureAvailability, "bot token could not be read")
	}
	if credentials.Validate() != nil || !botTokenPattern.MatchString(credentials.BotToken.Reveal()) {
		return localSendFailure(fence, hasPossibleEarlierSend, true, sdkgo.FailureAuthentication, "bot token is invalid")
	}
	token := credentials.BotToken.Reveal()
	form := url.Values{"chat_id": {input.ChatID}, "text": {input.Text}}.Encode()
	endpoint := operation.client.baseURL + "/bot" + token + "/sendMessage"
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, endpoint, strings.NewReader(form))
	if err != nil {
		return localSendFailure(fence, hasPossibleEarlierSend, true, sdkgo.FailureLocalDefect, "Telegram send request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return localSendFailure(fence, hasPossibleEarlierSend, true, sdkgo.FailureLocalDefect, "Telegram send request is invalid")
	}
	observation := &dispatchObservation{}
	var response *http.Response
	var body []byte
	var readErr error
	err = fence.Dispatch(func(requestContext context.Context) error {
		request = request.WithContext(httptrace.WithClientTrace(requestContext, observation.clientTrace()))
		var sendErr error
		response, sendErr = operation.client.httpClient.Do(request)
		if sendErr != nil {
			return sendErr
		}
		// Dispatch cancels requestContext on return, so the body is read here; its status classifies a read failure.
		body, readErr = providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
		return nil
	})
	if err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return retrySend(sdkgo.FailureTransport, "Telegram dispatch was not started", 0)
		}
		if observation.isProvablyUndispatched() {
			// A lost canResend checkpoint leaves the claim, which the next attempt counts as a send.
			_ = fence.RecordCanResend()
			return retrySend(sdkgo.FailureTransport, "Telegram could not be reached before sending", 0)
		}
		return resendOrSelectUncertain(canResendUnknownOutcome, sdkgo.FailureTransport, "Telegram message outcome is unknown", 0)
	}
	// Closing the response cannot change the confirmed provider outcome.
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return retrySend(sdkgo.FailureAvailability, "Telegram retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return retrySend(kind, "Telegram did not process the send", retryAfterFromHeaderOrBody(response.Header.Get("Retry-After"), body, readErr))
	}
	isSuccessStatus := response.StatusCode >= 200 && response.StatusCode < 300
	isBodyReadable := readErr == nil
	var envelope struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	// Telegram reports every refusal with an error status, so ok:true or a 2xx means it created the message.
	isAccepted := isBodyReadable && json.Unmarshal(body, &envelope) == nil && envelope.OK
	if isSuccessStatus && !isBodyReadable {
		return uncertainSend(sdkgo.FailureTransport, "Telegram accepted the message, but its response could not be read")
	}
	if isSuccessStatus && !isAccepted {
		return uncertainSend(sdkgo.FailureProtocol, "Telegram accepted the message, but its response is malformed")
	}
	if !isSuccessStatus && isAccepted {
		// The body reports acceptance, so a resend could duplicate the message.
		return uncertainSend(sdkgo.FailureProtocol, "Telegram returned a contradictory send response")
	}
	if !isSuccessStatus {
		// An unreadable or oversized error body cannot prove a refusal.
		if isBodyReadable && isDefiniteRefusalStatus(response.StatusCode) {
			return rejectOrSelectUncertain(fence, hasPossibleEarlierSend, rejectionFailureKind(response.StatusCode), "Telegram rejected the message with HTTP "+strconv.Itoa(response.StatusCode))
		}
		return resendOrSelectUncertain(canResendUnknownOutcome, sdkgo.FailureAvailability, "Telegram message outcome is unknown after HTTP "+strconv.Itoa(response.StatusCode), 0)
	}
	var result struct {
		MessageID int64 `json:"message_id"`
		Date      int64 `json:"date"`
		Chat      struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	}
	if json.Unmarshal(envelope.Result, &result) != nil || result.MessageID <= 0 || result.Chat.ID == 0 || result.Date <= 0 {
		// Telegram reported acceptance, so a resend could duplicate the message.
		return uncertainSend(sdkgo.FailureProtocol, "Telegram send response is incomplete")
	}
	message := SentMessage{ChatID: result.Chat.ID, MessageID: result.MessageID, Date: result.Date}
	// A lost provider-ID checkpoint leaves the claim, which still counts this send.
	_ = fence.RecordProviderID(strconv.FormatInt(message.MessageID, 10))
	return sdkgo.NewMutationBranch(SendMessageBranchSent, message, nil, sdkgo.Receipt{})
}

// isDefiniteRefusalStatus reports the HTTP statuses that prove Telegram refused the request.
func isDefiniteRefusalStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return true
	}
	return false
}

func rejectionFailureKind(statusCode int) sdkgo.FailureKind {
	switch statusCode {
	case http.StatusUnauthorized:
		return sdkgo.FailureAuthentication
	case http.StatusForbidden:
		return sdkgo.FailureAuthorization
	}
	return sdkgo.FailureProviderRejection
}

// resendOrSelectUncertain retries a possibly sent message while the send cap allows another send.
func resendOrSelectUncertain(canResendUnknownOutcome bool, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[SentMessage] {
	if canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt counts this send.
		return retrySend(kind, message, retryAfter)
	}
	return uncertainSend(kind, message)
}

// rejectOrSelectUncertain selects providerRejected only when no earlier send of this call may have created the message.
func rejectOrSelectUncertain(fence *singledispatch.Fence, hasPossibleEarlierSend bool, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentMessage] {
	if hasPossibleEarlierSend {
		return uncertainSend(kind, message)
	}
	// The branch is terminal, so a lost canResend checkpoint cannot cause a resend.
	_ = fence.RecordCanResend()
	return failedSend(SendMessageBranchProviderRejected, kind, message)
}

// localSendFailure returns Retry for a transient failure, recording notSent only when no earlier
// send may have created the message. A terminal failure selects defect before any possible send
// and uncertain after one.
func localSendFailure(fence *singledispatch.Fence, hasPossibleEarlierSend bool, isTerminal bool, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentMessage] {
	if !isTerminal {
		if !hasPossibleEarlierSend {
			// Without notSent the next attempt counts this attempt as a possible send, which only lowers the cap.
			_ = fence.RecordNotSent()
		}
		// After a possible send, notSent would hide that send, so nothing is recorded.
		return retrySend(kind, message+"; no message was sent in this attempt", 0)
	}
	if hasPossibleEarlierSend {
		return uncertainSend(kind, message+"; an earlier attempt may have sent the message")
	}
	// The branch is terminal, so a lost notSent checkpoint cannot cause a resend.
	_ = fence.RecordNotSent()
	return failedSend(SendMessageBranchDefect, kind, message)
}

// isTerminalCredentialError reports a credential failure that only reconnecting the connection can fix.
func isTerminalCredentialError(err error) bool {
	return errors.Is(err, sdkgo.ErrReauthorizationRequired) || errors.Is(err, projectconfig.ErrObjectNotFound) ||
		errors.Is(err, projectconfig.ErrConnectionNotSelected) || errors.Is(err, projectconfig.ErrCredentialBrokerRejected)
}

func failedSend(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentMessage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "telegram", Operation: "sendMessage", Message: message}
	return sdkgo.NewMutationBranch(branch, SentMessage{}, &failure, sdkgo.Receipt{})
}

func retrySend(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[SentMessage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "telegram", Operation: "sendMessage", Message: message}
	return sdkgo.NewMutationRetry[SentMessage](failure, retryAfter)
}

func uncertainSend(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentMessage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "telegram", Operation: "sendMessage", Message: message}
	return sdkgo.NewMutationUncertain(SentMessage{}, failure, sdkgo.Receipt{})
}
