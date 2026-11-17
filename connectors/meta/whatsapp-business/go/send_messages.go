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

// Invoke sends under a single-dispatch fence; an unknown outcome resends below call.MaxSends().
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

// Invoke sends an approved template under a single-dispatch fence; an unknown outcome resends below call.MaxSends().
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
	client      *http.Client
	request     *http.Request
	response    *http.Response
	observation *dispatchObservation
}

func (dispatch *messageDispatch) send(requestContext context.Context) error {
	request := dispatch.request.WithContext(httptrace.WithClientTrace(requestContext, dispatch.observation.clientTrace()))
	response, err := dispatch.client.Do(request)
	dispatch.response = response
	return err
}

// messageSend holds one attempt's send-cap decisions for classifying its outcome.
type messageSend struct {
	operationName           string
	fence                   *singledispatch.Fence
	canResendUnknownOutcome bool
	hasPossibleEarlierSend  bool
}

// sendMessage dispatches one Cloud API message. Meta has no send idempotency key or read-back,
// so an unknown outcome resends while possible sends stay below call.MaxSends() and selects
// uncertain at the cap. A checkpointed wamid completes without resending, and a definite
// refusal is never resent.
func (client *Client) sendMessage(call sdkgo.Call, operationName, phoneNumberID string, payload any, executeTimeout time.Duration) sdkgo.MutationAttempt[SentMessage] {
	body, err := json.Marshal(payload)
	if err != nil || len(body) > 65536 {
		return messageBranch(operationName, "defect", sdkgo.FailureValidation, "message request is invalid or too large")
	}
	fence, err := singledispatch.NewFence(call, executeTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return messageBranch(operationName, "defect", sdkgo.FailureLocalDefect, "Meta dispatch fence is unavailable")
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		return retryMessage(operationName, sdkgo.FailureAvailability, "Meta dispatch checkpoint is unreadable; no message was sent in this attempt", 0)
	case singledispatch.ReadBackRequired, singledispatch.ReadBackThenResendAllowed:
		// Meta returned this wamid only for a message it accepted, so the message is never resent.
		if isMessageID(decision.Marker.ProviderID) {
			return sdkgo.NewMutationBranch(sdkgo.BranchID("sent"), SentMessage{MessageID: decision.Marker.ProviderID}, nil, sdkgo.Receipt{})
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return uncertainMessage(operationName, sdkgo.FailureTransport, "an earlier attempt may have sent the WhatsApp message, and the send cap is reached")
		}
	}
	attempt := messageSend{
		operationName: operationName, fence: fence,
		// This attempt's send would be the last one the cap allows when no further send remains.
		canResendUnknownOutcome: decision.MaximumPriorSends+1 < call.MaxSends(),
		// An earlier send may have delivered, so a refusal of this resend cannot prove nothing changed.
		hasPossibleEarlierSend: decision.MaximumPriorSends > 0,
	}
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return attempt.localFailure(sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+"/"+phoneNumberID+"/messages", bytes.NewReader(body))
	if err != nil {
		return attempt.localFailure(sdkgo.FailureLocalDefect, "message request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return attempt.localFailure(sdkgo.FailureLocalDefect, "message request is invalid")
	}
	dispatch := &messageDispatch{client: client.httpClient, request: request, observation: &dispatchObservation{}}
	err = fence.Dispatch(dispatch.send)
	if err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return retryMessage(operationName, sdkgo.FailureTransport, "Meta dispatch was not started", 0)
		}
		if dispatch.observation.isProvablyUndispatched() {
			// A lost canResend checkpoint leaves the claim, which the next attempt counts as a send.
			_ = fence.RecordCanResend()
			return retryMessage(operationName, sdkgo.FailureTransport, "Meta could not be reached before sending", 0)
		}
		return attempt.resendOrSelectUncertain(sdkgo.FailureTransport, "Meta message outcome is unknown", 0)
	}
	response := dispatch.response
	defer func() { _ = response.Body.Close() }()
	return attempt.classifyResponse(response, client.maxResponseBytes)
}

func (attempt messageSend) classifyResponse(response *http.Response, maxResponseBytes int64) sdkgo.MutationAttempt[SentMessage] {
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	responseBody, readErr := providerhttp.ReadBoundedBody(response.Body, maxResponseBytes)
	isClientError := response.StatusCode >= 400 && response.StatusCode < 500
	// A Graph or Cloud API throttling code means Meta refused the send before processing it.
	isRateLimited := response.StatusCode == http.StatusTooManyRequests || (isClientError && readErr == nil && isGraphThrottleBody(responseBody))
	if isRateLimited || response.StatusCode == http.StatusServiceUnavailable {
		if err := attempt.fence.RecordCanResend(); err != nil {
			return retryMessage(attempt.operationName, sdkgo.FailureAvailability, "Meta retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if isRateLimited {
			kind = sdkgo.FailureRateLimit
		}
		return retryMessage(attempt.operationName, kind, "Meta did not process the send", retryAfter)
	}
	acceptedMessageID, isComplete := "", false
	if readErr == nil {
		acceptedMessageID, isComplete = readSentMessageID(responseBody)
	}
	if isDefiniteRefusalStatus(response.StatusCode) {
		if acceptedMessageID != "" {
			// The body reports an accepted message, so a resend could duplicate it.
			return uncertainMessage(attempt.operationName, sdkgo.FailureProtocol, "Meta returned a contradictory send response")
		}
		return attempt.rejectOrSelectUncertain(rejectionFailureKind(response.StatusCode), "Meta rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return attempt.resendOrSelectUncertain(sdkgo.FailureAvailability, "Meta message outcome is unknown after HTTP "+strconv.Itoa(response.StatusCode), retryAfter)
	}
	if readErr != nil {
		return attempt.resendOrSelectUncertain(sdkgo.FailureTransport, "Meta send response could not be read", 0)
	}
	if acceptedMessageID == "" {
		return attempt.resendOrSelectUncertain(sdkgo.FailureProtocol, "Meta send response is malformed", 0)
	}
	if !isComplete {
		// Meta returned a wamid, so a resend could duplicate the message.
		return uncertainMessage(attempt.operationName, sdkgo.FailureProtocol, "Meta send response is incomplete")
	}
	// A lost provider-ID checkpoint leaves the claim, which still counts this send.
	_ = attempt.fence.RecordProviderID(acceptedMessageID)
	return sdkgo.NewMutationBranch(sdkgo.BranchID("sent"), SentMessage{MessageID: acceptedMessageID}, nil, sdkgo.Receipt{})
}

// resendOrSelectUncertain retries a possibly sent message while the send cap allows another send.
func (attempt messageSend) resendOrSelectUncertain(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[SentMessage] {
	if attempt.canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt counts this send.
		return retryMessage(attempt.operationName, kind, message, retryAfter)
	}
	return uncertainMessage(attempt.operationName, kind, message)
}

// rejectOrSelectUncertain selects providerRejected only when no earlier send may have delivered.
func (attempt messageSend) rejectOrSelectUncertain(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentMessage] {
	if attempt.hasPossibleEarlierSend {
		return uncertainMessage(attempt.operationName, kind, message)
	}
	// The branch is terminal, so a lost canResend checkpoint cannot cause a resend.
	_ = attempt.fence.RecordCanResend()
	return messageBranch(attempt.operationName, "providerRejected", kind, message)
}

// localFailure selects defect only when no earlier send may have delivered.
func (attempt messageSend) localFailure(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentMessage] {
	if attempt.hasPossibleEarlierSend {
		// notSent would hide the earlier possible send, so nothing is recorded.
		return retryMessage(attempt.operationName, kind, message+"; no message was sent in this attempt", 0)
	}
	// The branch is terminal, so a lost notSent checkpoint cannot cause a resend.
	_ = attempt.fence.RecordNotSent()
	return messageBranch(attempt.operationName, "defect", kind, message)
}

// readSentMessageID returns the first wamid in a send response and whether the response is complete.
func readSentMessageID(body []byte) (string, bool) {
	var envelope struct {
		MessagingProduct string `json:"messaging_product"`
		Messages         []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &envelope) != nil || len(envelope.Messages) == 0 || !isMessageID(envelope.Messages[0].ID) {
		return "", false
	}
	return envelope.Messages[0].ID, envelope.MessagingProduct == "whatsapp" && len(envelope.Messages) == 1
}

func isMessageID(value string) bool {
	return strings.HasPrefix(value, "wamid.") && len(value) > len("wamid.")
}

// isDefiniteRefusalStatus reports the HTTP statuses that prove Meta refused the request.
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
