// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package whatsappbusiness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptrace"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// SendTextMessageInput selects a registered sender, recipient, and plain-text message.
type SendTextMessageInput struct {
	// PhoneNumberID is the numeric sender ID returned by listPhoneNumbers.
	PhoneNumberID string `json:"phoneNumberId"`
	// Recipient is the customer's E.164 phone number without the leading plus sign.
	Recipient string `json:"recipient"`
	// Text is nonempty UTF-8 text, sent only inside the customer service window.
	Text string `json:"text"`
}

// SendTemplateMessageInput selects an approved template and optional text body parameters.
type SendTemplateMessageInput struct {
	// PhoneNumberID is the numeric sender ID returned by listPhoneNumbers.
	PhoneNumberID string `json:"phoneNumberId"`
	// Recipient is the customer's E.164 phone number without the leading plus sign.
	Recipient string `json:"recipient"`
	// TemplateName is the approved template name in WhatsApp Manager.
	TemplateName string `json:"templateName"`
	// LanguageCode is the approved template language code, such as en_US.
	LanguageCode string `json:"languageCode"`
	// BodyParameters replace text placeholders in template body order.
	BodyParameters []string `json:"bodyParameters,omitempty"`
}

// SentMessage identifies a WhatsApp message accepted by Meta.
type SentMessage struct {
	// MessageID is the provider's wamid identifier; acceptance does not imply delivery.
	MessageID string `json:"messageId"`
}

// SendTextMessageOperation sends plain text through one durable dispatch fence.
type SendTextMessageOperation struct{ client *Client }

// SendTextMessage binds the text Mutation to this client.
func (client *Client) SendTextMessage() SendTextMessageOperation {
	return SendTextMessageOperation{client}
}

// Definition returns the manifest-generated text Mutation contract.
func (SendTextMessageOperation) Definition() sdkgo.MutationDefinition {
	return SendTextMessageDefinition
}

// IdempotencyKey records the stable call identity; Meta does not deduplicate on this key.
func (SendTextMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ SendTextMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends once and fails closed when the provider outcome is ambiguous.
func (operation SendTextMessageOperation) Invoke(call sdkgo.Call, input SendTextMessageInput) sdkgo.MutationAttempt[SentMessage] {
	if !validMessageAddress(input.PhoneNumberID, input.Recipient) || input.Text == "" || !utf8.ValidString(input.Text) || len(input.Text) > 4096 {
		return messageBranch("sendTextMessage", "defect", sdkgo.FailureValidation, "sender, recipient, or text is invalid")
	}
	payload := map[string]any{"messaging_product": "whatsapp", "recipient_type": "individual", "to": input.Recipient, "type": "text", "text": map[string]any{"preview_url": false, "body": input.Text}}
	return operation.client.sendMessage(call, "sendTextMessage", input.PhoneNumberID, payload, SendTextMessageDefinition.StepDefaults.ExecuteMethodTimeout)
}

// SendTemplateMessageOperation sends an approved template through one durable dispatch fence.
type SendTemplateMessageOperation struct{ client *Client }

// SendTemplateMessage binds the template Mutation to this client.
func (client *Client) SendTemplateMessage() SendTemplateMessageOperation {
	return SendTemplateMessageOperation{client}
}

// Definition returns the manifest-generated template Mutation contract.
func (SendTemplateMessageOperation) Definition() sdkgo.MutationDefinition {
	return SendTemplateMessageDefinition
}

// IdempotencyKey records the stable call identity; Meta does not deduplicate on this key.
func (SendTemplateMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ SendTemplateMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

var templateNamePattern = regexp.MustCompile(`^[a-z0-9_]{1,512}$`)
var languageCodePattern = regexp.MustCompile(`^[a-z]{2,3}(?:_[A-Z]{2})?$`)
var recipientPattern = regexp.MustCompile(`^[1-9][0-9]{5,14}$`)

// Invoke sends an approved template once and fails closed on ambiguous delivery.
func (operation SendTemplateMessageOperation) Invoke(call sdkgo.Call, input SendTemplateMessageInput) sdkgo.MutationAttempt[SentMessage] {
	if !validMessageAddress(input.PhoneNumberID, input.Recipient) || !templateNamePattern.MatchString(input.TemplateName) || !languageCodePattern.MatchString(input.LanguageCode) || len(input.BodyParameters) > 50 {
		return messageBranch("sendTemplateMessage", "defect", sdkgo.FailureValidation, "sender, recipient, or template is invalid")
	}
	template := map[string]any{"name": input.TemplateName, "language": map[string]string{"code": input.LanguageCode}}
	if len(input.BodyParameters) > 0 {
		parameters := make([]map[string]string, 0, len(input.BodyParameters))
		for _, value := range input.BodyParameters {
			if value == "" || !utf8.ValidString(value) || len(value) > 1024 {
				return messageBranch("sendTemplateMessage", "defect", sdkgo.FailureValidation, "template body parameter is invalid")
			}
			parameters = append(parameters, map[string]string{"type": "text", "text": value})
		}
		template["components"] = []any{map[string]any{"type": "body", "parameters": parameters}}
	}
	payload := map[string]any{"messaging_product": "whatsapp", "recipient_type": "individual", "to": input.Recipient, "type": "template", "template": template}
	return operation.client.sendMessage(call, "sendTemplateMessage", input.PhoneNumberID, payload, SendTemplateMessageDefinition.StepDefaults.ExecuteMethodTimeout)
}

func validMessageAddress(phoneNumberID, recipient string) bool {
	return numericID.MatchString(phoneNumberID) && recipientPattern.MatchString(recipient)
}

type messageDispatch struct {
	client           *http.Client
	request          *http.Request
	response         *http.Response
	didGetConnection atomic.Bool
}

func (dispatch *messageDispatch) send(requestContext context.Context) error {
	trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { dispatch.didGetConnection.Store(true) }}
	request := dispatch.request.WithContext(httptrace.WithClientTrace(requestContext, trace))
	response, err := dispatch.client.Do(request)
	dispatch.response = response
	return err
}

func (client *Client) sendMessage(call sdkgo.Call, operationName, phoneNumberID string, payload any, executeTimeout time.Duration) sdkgo.MutationAttempt[SentMessage] {
	fence, err := singledispatch.NewFence(call, executeTimeout)
	if err != nil {
		return messageBranch(operationName, "defect", sdkgo.FailureLocalDefect, "Meta dispatch fence is unavailable")
	}
	if fence.Decide().Kind != singledispatch.SendAllowed {
		return uncertainMessage(operationName, sdkgo.FailureTransport, "Meta send outcome cannot be confirmed; do not resend")
	}
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return messageBranch(operationName, "defect", sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	accessToken := credentials.AccessToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(accessToken) {
		return messageBranch(operationName, "defect", sdkgo.FailureAuthentication, "connection credentials are invalid")
	}
	body, err := json.Marshal(payload)
	if err != nil || len(body) > 65536 {
		return messageBranch(operationName, "defect", sdkgo.FailureValidation, "message request is invalid or too large")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+"/"+phoneNumberID+"/messages", bytes.NewReader(body))
	if err != nil {
		return messageBranch(operationName, "defect", sdkgo.FailureLocalDefect, "message request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return messageBranch(operationName, "defect", sdkgo.FailureLocalDefect, "message request is invalid")
	}
	dispatch := &messageDispatch{client: client.httpClient, request: request}
	err = fence.Dispatch(dispatch.send)
	if err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return retryMessage(operationName, sdkgo.FailureTransport, "Meta dispatch was not started", 0)
		}
		if !dispatch.didGetConnection.Load() {
			if checkpointErr := fence.RecordCanResend(); checkpointErr == nil {
				return retryMessage(operationName, sdkgo.FailureTransport, "Meta could not be reached before sending", 0)
			}
		}
		return uncertainMessage(operationName, sdkgo.FailureTransport, "Meta message outcome is unknown")
	}
	response := dispatch.response
	defer func() { _ = response.Body.Close() }()
	isRateLimited := response.StatusCode == http.StatusTooManyRequests
	if !isRateLimited && response.StatusCode >= 400 && response.StatusCode < 500 {
		// A Graph or Cloud API throttling code means Meta refused the send before processing it.
		isRateLimited = isGraphThrottleResponse(response, client.maxResponseBytes)
	}
	if isRateLimited || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return retryMessage(operationName, sdkgo.FailureAvailability, "Meta retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if isRateLimited {
			kind = sdkgo.FailureRateLimit
		}
		return retryMessage(operationName, kind, "Meta did not process the send", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode >= 500 || response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict {
		return uncertainMessage(operationName, sdkgo.FailureAvailability, "Meta message outcome is unknown")
	}
	if response.StatusCode >= 400 && response.StatusCode < 500 {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return messageBranch(operationName, "providerRejected", kind, "Meta rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return uncertainMessage(operationName, sdkgo.FailureTransport, "Meta send response could not be confirmed")
	}
	var envelope struct {
		MessagingProduct string `json:"messaging_product"`
		Messages         []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if response.StatusCode != http.StatusOK || json.Unmarshal(responseBody, &envelope) != nil || envelope.MessagingProduct != "whatsapp" || len(envelope.Messages) != 1 || !strings.HasPrefix(envelope.Messages[0].ID, "wamid.") {
		return uncertainMessage(operationName, sdkgo.FailureProtocol, "Meta send response is incomplete")
	}
	message := SentMessage{MessageID: envelope.Messages[0].ID}
	if err := fence.RecordProviderID(message.MessageID); err != nil {
		return uncertainMessage(operationName, sdkgo.FailureLocalDefect, "Meta message ID could not be checkpointed")
	}
	return sdkgo.NewMutationBranch(sdkgo.BranchID("sent"), message, nil, sdkgo.Receipt{})
}

func messageFailure(operationName string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "whatsapp-business", Operation: operationName, Message: message}
}

func messageBranch(operationName string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentMessage] {
	failure := messageFailure(operationName, kind, message)
	return sdkgo.NewMutationBranch(branch, SentMessage{}, &failure, sdkgo.Receipt{})
}

func retryMessage(operationName string, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[SentMessage] {
	return sdkgo.NewMutationRetry[SentMessage](messageFailure(operationName, kind, message), retryAfter)
}

func uncertainMessage(operationName string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentMessage] {
	return sdkgo.NewMutationUncertain(SentMessage{}, messageFailure(operationName, kind, message), sdkgo.Receipt{})
}
