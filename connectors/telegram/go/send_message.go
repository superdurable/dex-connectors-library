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
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
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

// Invoke sends once, retries only proven non-processed requests, and fails closed after ambiguous dispatch.
func (operation SendMessageOperation) Invoke(call sdkgo.Call, input SendMessageInput) sdkgo.MutationAttempt[SentMessage] {
	fence, err := singledispatch.NewFence(call, SendMessageDefinition.StepDefaults.ExecuteMethodTimeout)
	if err != nil {
		return failedSend(SendMessageBranchDefect, sdkgo.FailureLocalDefect, "Telegram dispatch fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind != singledispatch.SendAllowed {
		return uncertainSend(sdkgo.FailureTransport, "Telegram send outcome cannot be confirmed; do not resend")
	}
	if !validChatID(input.ChatID) || input.Text == "" || utf8.RuneCountInString(input.Text) > 4096 || !utf8.ValidString(input.Text) {
		return failedSend(SendMessageBranchDefect, sdkgo.FailureValidation, "chatId or text is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return failedSend(SendMessageBranchDefect, sdkgo.FailureAuthentication, "bot token is unavailable or invalid")
	}
	token := credentials.BotToken.Reveal()
	if !botTokenPattern.MatchString(token) {
		return failedSend(SendMessageBranchDefect, sdkgo.FailureAuthentication, "bot token is invalid")
	}
	form := url.Values{"chat_id": {input.ChatID}, "text": {input.Text}}.Encode()
	endpoint := operation.client.baseURL + "/bot" + token + "/sendMessage"
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, endpoint, strings.NewReader(form))
	if err != nil {
		return failedSend(SendMessageBranchDefect, sdkgo.FailureLocalDefect, "Telegram send request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return failedSend(SendMessageBranchDefect, sdkgo.FailureLocalDefect, "Telegram send request is invalid")
	}
	var didGetConnection atomic.Bool
	var response *http.Response
	err = fence.Dispatch(func(requestContext context.Context) error {
		trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { didGetConnection.Store(true) }}
		request = request.WithContext(httptrace.WithClientTrace(requestContext, trace))
		response, err = operation.client.httpClient.Do(request)
		return err
	})
	if err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return retrySend(sdkgo.FailureTransport, "Telegram dispatch was not started", 0)
		}
		if !didGetConnection.Load() {
			if checkpointErr := fence.RecordCanResend(); checkpointErr == nil {
				return retrySend(sdkgo.FailureTransport, "Telegram could not be reached before sending", 0)
			}
		}
		return uncertainSend(sdkgo.FailureTransport, "Telegram message outcome is unknown")
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
		return retrySend(kind, "Telegram did not process the send", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode >= 500 || response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict {
		return uncertainSend(sdkgo.FailureAvailability, "Telegram message outcome is unknown")
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return uncertainSend(sdkgo.FailureTransport, "Telegram send response could not be confirmed")
	}
	var envelope struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageID int64 `json:"message_id"`
			Date      int64 `json:"date"`
			Chat      struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"result"`
		ErrorCode int `json:"error_code"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return uncertainSend(sdkgo.FailureProtocol, "Telegram send response is malformed")
	}
	if response.StatusCode >= 400 && response.StatusCode < 500 && !envelope.OK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return failedSend(SendMessageBranchProviderRejected, kind, "Telegram rejected the message with HTTP "+strconv.Itoa(response.StatusCode))
	}
	if response.StatusCode != http.StatusOK || !envelope.OK || envelope.Result.MessageID <= 0 || envelope.Result.Chat.ID == 0 || envelope.Result.Date <= 0 {
		return uncertainSend(sdkgo.FailureProtocol, "Telegram send response is incomplete")
	}
	message := SentMessage{ChatID: envelope.Result.Chat.ID, MessageID: envelope.Result.MessageID, Date: envelope.Result.Date}
	// A lost provider-ID checkpoint still never permits a resend.
	_ = fence.RecordProviderID(strconv.FormatInt(message.MessageID, 10))
	return sdkgo.NewMutationBranch(SendMessageBranchSent, message, nil, sdkgo.Receipt{})
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
