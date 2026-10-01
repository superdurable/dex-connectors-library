// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webhook

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// maxDrainedResponseBytes bounds the response body read so the connection can be reused.
const maxDrainedResponseBytes = 64 << 10

var errSendEventRequestInvalid = errors.New("sendEvent request is invalid")

// SendEventInput is one JSON event that sendEvent POSTs to the connection's deliveryUrl.
type SendEventInput struct {
	// Payload is the complete JSON request body, such as {"type":"form.submitted","data":{...}}. It must be
	// one valid JSON value of at most maxBodyBytes bytes; an empty or invalid payload selects defect.
	Payload json.RawMessage `json:"payload"`
}

// SendEventOutput describes one sendEvent attempt's outcome. It never holds the payload or response body.
type SendEventOutput struct {
	// WebhookID is the webhook-id header value. It is derived from the Step's idempotency key, so every
	// retry and duplicate dispatch of one Step execution sends the same ID and receivers can deduplicate.
	WebhookID string `json:"webhookId"`
	// StatusCode is the receiver's HTTP status, or zero when no response arrived.
	StatusCode int `json:"statusCode,omitempty"`
}

// SendEventOperation implements the sendEvent Mutation. Build it with Client.SendEvent.
type SendEventOperation struct{ client *Client }

// Definition returns the immutable sendEvent operation definition.
func (SendEventOperation) Definition() sdkgo.MutationDefinition {
	return SendEventDefinition
}

// IdempotencyKey returns the Standard Webhooks webhook-id: msg_ followed by the call ID's 32 hex digits.
func (SendEventOperation) IdempotencyKey(callID sdkgo.CallID, _ SendEventInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey("msg_" + strings.ReplaceAll(string(callID), "-", ""))
}

// Invoke POSTs the payload once with webhook-id, webhook-timestamp, and webhook-signature headers. A 2xx
// selects delivered, and a redirect or a 4xx other than 408 and 429 selects rejected. A 408 or 429, or a
// request that never left this process, returns Retry with the same webhook-id. A 5xx, timeout, or lost
// connection after the request was written selects uncertain, because the receiver may have accepted it.
func (operation SendEventOperation) Invoke(call sdkgo.Call, input SendEventInput) sdkgo.MutationAttempt[SendEventOutput] {
	client := operation.client
	output := SendEventOutput{WebhookID: string(call.IdempotencyKey)}
	if client.deliveryURL == "" {
		return sendEventDefect(output, sdkgo.FailureValidation,
			"the connection has no deliveryUrl; set it in Dex Web Connectors to the receiver's HTTPS webhook URL")
	}
	if len(input.Payload) == 0 || !json.Valid(input.Payload) {
		return sendEventDefect(output, sdkgo.FailureValidation, "sendEvent payload must be one valid JSON value")
	}
	if int64(len(input.Payload)) > client.maxBodyBytes {
		return sendEventDefect(output, sdkgo.FailureValidation, "sendEvent payload exceeds the connection's maxBodyBytes")
	}
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.SigningSecret.Reveal() == "" {
		return sendEventDefect(output, sdkgo.FailureAuthentication, "the webhook signing secret is unavailable")
	}
	key, err := standardWebhooksSigningKey(credentials.SigningSecret.Reveal())
	if err != nil {
		return sendEventDefect(output, sdkgo.FailureValidation, err.Error())
	}
	var isDispatched atomic.Bool
	response, err := operation.postSignedEvent(call.Context, &isDispatched, key, output.WebhookID, input.Payload)
	if err != nil {
		if errors.Is(err, errSendEventRequestInvalid) {
			return sendEventDefect(output, sdkgo.FailureLocalDefect, "the sendEvent request could not be built")
		}
		if !isDispatched.Load() {
			return sdkgo.NewMutationRetry[SendEventOutput](sendEventFailure(sdkgo.FailureAvailability, "the delivery URL could not be reached"), 0)
		}
		return sdkgo.NewMutationUncertain(output, sendEventFailure(sdkgo.FailureTransport,
			"the event was sent but the receiver did not answer"), operation.sendEventReceipt(output))
	}
	defer drainAndClose(response.Body)
	output.StatusCode = response.StatusCode
	status := response.StatusCode
	switch {
	case status >= 200 && status < 300:
		return sdkgo.NewMutationBranch(SendEventBranchDelivered, output, nil, operation.sendEventReceipt(output))
	case status == http.StatusRequestTimeout || status == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[SendEventOutput](sendEventFailure(sdkgo.FailureRateLimit,
			fmt.Sprintf("the receiver answered HTTP %d", status)), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), client.now()))
	case status >= 500:
		return sdkgo.NewMutationUncertain(output, sendEventFailure(sdkgo.FailureAvailability,
			fmt.Sprintf("the receiver answered HTTP %d after the event was sent", status)), operation.sendEventReceipt(output))
	default:
		failure := sendEventFailure(rejectionFailureKind(status), fmt.Sprintf("the receiver answered HTTP %d", status))
		return sdkgo.NewMutationBranch(SendEventBranchRejected, output, &failure, operation.sendEventReceipt(output))
	}
}

// postSignedEvent records in isDispatched whether the request was written before any failure.
func (operation SendEventOperation) postSignedEvent(
	requestContext context.Context, isDispatched *atomic.Bool, key []byte, webhookID string, payload []byte,
) (*http.Response, error) {
	tracedContext := httptrace.WithClientTrace(requestContext, &httptrace.ClientTrace{
		WroteRequest: func(written httptrace.WroteRequestInfo) {
			if written.Err == nil {
				isDispatched.Store(true)
			}
		},
	})
	client := operation.client
	request, err := http.NewRequestWithContext(tracedContext, http.MethodPost, client.deliveryURL, bytes.NewReader(payload))
	if err != nil {
		return nil, errSendEventRequestInvalid
	}
	timestampText := strconv.FormatInt(client.now().Unix(), 10)
	signature := standardWebhooksSignature(key, webhookID, timestampText, payload)
	request.Header.Set("Content-Type", ContentTypeJSON)
	request.Header.Set(standardWebhooksIDHeader, webhookID)
	request.Header.Set(standardWebhooksTimestampHeader, timestampText)
	request.Header.Set(standardWebhooksSignatureHeader, standardWebhooksSignatureScheme+","+base64.StdEncoding.EncodeToString(signature))
	return client.httpClient.Do(request)
}

func (operation SendEventOperation) sendEventReceipt(output SendEventOutput) sdkgo.Receipt {
	return sdkgo.Receipt{Provider: ConnectorID, ProviderObjectID: output.WebhookID, ObservedAt: operation.client.now().UTC()}
}

// validateDeliveryURL accepts blank, or an absolute HTTPS URL without user information or a fragment.
func validateDeliveryURL(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	for index := 0; index < len(value); index++ {
		if value[index] <= ' ' || value[index] > '~' {
			return "", fmt.Errorf("webhook deliveryUrl must be printable ASCII without spaces")
		}
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return "", fmt.Errorf("webhook deliveryUrl must be an absolute HTTPS URL")
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return "", fmt.Errorf("webhook deliveryUrl cannot contain user information or a fragment")
	}
	return parsed.String(), nil
}

func rejectionFailureKind(status int) sdkgo.FailureKind {
	switch status {
	case http.StatusUnauthorized:
		return sdkgo.FailureAuthentication
	case http.StatusForbidden:
		return sdkgo.FailureAuthorization
	case http.StatusNotFound, http.StatusGone:
		return sdkgo.FailureNotFound
	case http.StatusConflict:
		return sdkgo.FailureConflict
	default:
		return sdkgo.FailureProviderRejection
	}
}

func sendEventDefect(output SendEventOutput, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SendEventOutput] {
	failure := sendEventFailure(kind, message)
	return sdkgo.NewMutationBranch(SendEventBranchDefect, output, &failure, sdkgo.Receipt{})
}

func sendEventFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: ConnectorID, Operation: SendEventDefinition.Operation.OperationID, Message: message}
}

func drainAndClose(body io.ReadCloser) {
	// Draining is best-effort; it only lets the transport reuse the connection.
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxDrainedResponseBytes))
	_ = body.Close()
}
