// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docusign

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

const (
	// ConnectEventEnvelopeCompleted is the Connect event sent when every recipient finished.
	ConnectEventEnvelopeCompleted = "envelope-completed"
	// ConnectEventEnvelopeDeclined is the Connect event sent when a recipient declined.
	ConnectEventEnvelopeDeclined = "envelope-declined"
	// ConnectEventEnvelopeVoided is the Connect event sent when the sender voided the envelope or it expired.
	ConnectEventEnvelopeVoided = "envelope-voided"

	// connectSignatureHeaderPrefix is the canonical form of X-DocuSign-Signature-1, -2, and so on: one
	// header per HMAC key of the account.
	connectSignatureHeaderPrefix = "X-Docusign-Signature-"
)

var errConnectSignatureInvalid = errors.New("no X-DocuSign-Signature header matches the HMAC-SHA256 of the body")

// connectEnvelopeEvents are the Connect events the envelopeEventReceived Trigger records.
var connectEnvelopeEvents = []string{ConnectEventEnvelopeCompleted, ConnectEventEnvelopeDeclined, ConnectEventEnvelopeVoided}

// EnvelopeEventReceivedTriggerConfiguration filters the Connect events one binding records. The zero
// value records every envelope-completed, envelope-declined, and envelope-voided event. It reduces what a
// binding stores; the application's TriggerFilter remains its admission rule.
type EnvelopeEventReceivedTriggerConfiguration struct {
	// Events lists envelope-completed, envelope-declined, and envelope-voided; empty accepts all three.
	Events []string `json:"events,omitempty"`
}

// EnvelopeEvent is one HMAC-verified Connect envelope event in the JSON SIM format. Its Trigger event ID
// is the envelope ID and the event name, such as 93be49ab-0000-0000-0000-f752070d71ec:envelope-completed,
// so a Connect retry or a second Connect configuration delivering the same outcome keeps one ID.
type EnvelopeEvent struct {
	// Event is envelope-completed, envelope-declined, or envelope-voided.
	Event string `json:"event"`
	// EnvelopeID is the envelope's GUID.
	EnvelopeID string `json:"envelopeId"`
	// AccountID is the sender's DocuSign API account.
	AccountID string `json:"accountId"`
	// GeneratedAt is when Connect generated the message, or when the endpoint received it if Connect
	// left the time blank.
	GeneratedAt time.Time `json:"generatedAt"`
	// Status is the envelope summary's status; blank unless the Connect configuration includes envelope data.
	Status EnvelopeStatus `json:"status,omitempty"`
	// VoidedReason is the envelope summary's void reason for an envelope-voided event, when included.
	VoidedReason string `json:"voidedReason,omitempty"`
	// CustomFields are the envelope's text custom fields; empty unless the Connect configuration's
	// Include Data lists Custom Fields.
	CustomFields []EnvelopeCustomField `json:"customFields,omitempty"`
}

type connectMessage struct {
	Event             string `json:"event"`
	GeneratedDateTime string `json:"generatedDateTime"`
	Data              *struct {
		AccountID  string `json:"accountId"`
		EnvelopeID string `json:"envelopeId"`
		// EnvelopeSummary is an object only when the configuration includes envelope data.
		EnvelopeSummary json.RawMessage `json:"envelopeSummary"`
	} `json:"data"`
}

// connectEnvelopeSummary is the part of a JSON SIM envelope summary the Trigger reads.
type connectEnvelopeSummary struct {
	Status       string                  `json:"status"`
	VoidedReason string                  `json:"voidedReason"`
	CustomFields *docusignCustomFieldSet `json:"customFields"`
}

// CustomFieldValue returns the value of the named text custom field.
func (event EnvelopeEvent) CustomFieldValue(name string) (string, bool) {
	for _, field := range event.CustomFields {
		if field.Name == name {
			return field.Value, true
		}
	}
	return "", false
}

// Validate checks that Events holds distinct supported Connect events.
func (configuration EnvelopeEventReceivedTriggerConfiguration) Validate() error {
	seen := map[string]bool{}
	for _, event := range configuration.Events {
		if !slices.Contains(connectEnvelopeEvents, event) || seen[event] {
			return fmt.Errorf("envelopeEventReceived events must be distinct values from %s", strings.Join(connectEnvelopeEvents, ", "))
		}
		seen[event] = true
	}
	return nil
}

// acceptsEvent applies the binding's event filter.
func (configuration EnvelopeEventReceivedTriggerConfiguration) acceptsEvent(event sdkgo.TriggerEvent[EnvelopeEvent]) bool {
	return len(configuration.Events) == 0 || slices.Contains(configuration.Events, event.Payload.Event)
}

// EnvelopeEventReceivedWebhookHandler returns the connection's Connect endpoint for an application to mount
// at the URL to Publish of its Connect configuration. Every envelopeEventReceived Trigger built from this
// Connection feeds from it, and it answers 503 while none of them runs, so Connect retries.
func (connection Connection) EnvelopeEventReceivedWebhookHandler() (http.Handler, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	return connection.client.envelopeEventReceivedWebhookEndpoint(connection.reference)
}

func (client *Client) envelopeEventReceivedTriggerSource(
	connection sdkgo.ConnectionRef,
	configuration EnvelopeEventReceivedTriggerConfiguration,
) sdkgo.TriggerSource[EnvelopeEvent] {
	if err := configuration.Validate(); err != nil {
		panic(err)
	}
	endpoint, err := client.envelopeEventReceivedWebhookEndpoint(connection)
	if err != nil {
		panic(err)
	}
	return endpoint.NewSource(configuration.acceptsEvent)
}

// envelopeEventReceivedWebhookEndpoint returns the connection's shared endpoint, creating it on first use.
func (client *Client) envelopeEventReceivedWebhookEndpoint(
	connection sdkgo.ConnectionRef,
) (*webhooktrigger.Endpoint[Credentials, EnvelopeEvent], error) {
	client.envelopeEventEndpointsMu.Lock()
	defer client.envelopeEventEndpointsMu.Unlock()
	if endpoint, isFound := client.envelopeEventEndpoints[connection]; isFound {
		return endpoint, nil
	}
	endpoint, err := webhooktrigger.NewEndpoint(webhooktrigger.EndpointConfig[Credentials, EnvelopeEvent]{
		ConnectorID: ConnectorID, TriggerName: EnvelopeEventReceivedTriggerDefinition.Trigger.TriggerName,
		Connection: connection, Credentials: client.credentials, CredentialRefresh: client.refreshDriver,
		MaxBodyBytes: client.connectMaxBodyBytes, VerifyRequest: verifyConnectRequest, DecodeEvent: decodeConnectRequest,
		Now: client.now, Logger: client.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("DocuSign Connect endpoint: %w", err)
	}
	client.envelopeEventEndpoints[connection] = endpoint
	return endpoint, nil
}

// verifyConnectRequest answers 503 without an HMAC key, so Connect keeps retrying the delivery.
func verifyConnectRequest(request webhooktrigger.Request, credentials Credentials) error {
	hmacKey := strings.ReplaceAll(credentials.ConnectHMACKey.Reveal(), `"`, "")
	if hmacKey == "" {
		return webhooktrigger.ErrVerificationUnavailable
	}
	return verifyConnectSignature(request.Body, request.Header, hmacKey)
}

// verifyConnectSignature accepts any X-DocuSign-Signature-N equal to the body's base64 HMAC-SHA256.
func verifyConnectSignature(body []byte, header http.Header, hmacKey string) error {
	mac := hmac.New(sha256.New, []byte(hmacKey))
	_, _ = mac.Write(body) // A hash never fails to write.
	expected := mac.Sum(nil)
	for name, values := range header {
		suffix, isSignature := strings.CutPrefix(http.CanonicalHeaderKey(name), connectSignatureHeaderPrefix)
		if _, err := strconv.ParseUint(suffix, 10, 16); !isSignature || err != nil {
			continue
		}
		for _, value := range values {
			signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
			if err == nil && hmac.Equal(signature, expected) {
				return nil
			}
		}
	}
	return errConnectSignatureInvalid
}

// decodeConnectRequest decodes a JSON SIM envelope outcome; other events are acknowledged unrecorded.
func decodeConnectRequest(request webhooktrigger.Request) (sdkgo.TriggerEvent[EnvelopeEvent], bool, error) {
	var message connectMessage
	if err := json.Unmarshal(request.Body, &message); err != nil || message.Event == "" || message.Data == nil {
		return sdkgo.TriggerEvent[EnvelopeEvent]{}, false, errDocuSignResponseMalformed
	}
	if !slices.Contains(connectEnvelopeEvents, message.Event) {
		return sdkgo.TriggerEvent[EnvelopeEvent]{}, false, nil
	}
	if !isDocuSignGUID(message.Data.EnvelopeID) || !isDocuSignGUID(message.Data.AccountID) {
		return sdkgo.TriggerEvent[EnvelopeEvent]{}, false, errDocuSignResponseMalformed
	}
	event := EnvelopeEvent{
		Event: message.Event, EnvelopeID: lowercaseGUID(message.Data.EnvelopeID), AccountID: lowercaseGUID(message.Data.AccountID),
		GeneratedAt: request.ReceivedAt.UTC(),
	}
	if generatedAt, err := parseOptionalDocuSignTime(message.GeneratedDateTime); err == nil && generatedAt != nil {
		event.GeneratedAt = *generatedAt
	}
	if summaryJSON := bytes.TrimSpace(message.Data.EnvelopeSummary); len(summaryJSON) > 0 && summaryJSON[0] == '{' {
		var summary connectEnvelopeSummary
		if err := json.Unmarshal(summaryJSON, &summary); err != nil {
			return sdkgo.TriggerEvent[EnvelopeEvent]{}, false, errDocuSignResponseMalformed
		}
		event.Status = EnvelopeStatus(strings.ToLower(summary.Status))
		event.VoidedReason = summary.VoidedReason
		event.CustomFields = summary.CustomFields.textFields()
	}
	return sdkgo.TriggerEvent[EnvelopeEvent]{
		ID: event.EnvelopeID + ":" + event.Event, OccurredAt: event.GeneratedAt, Payload: event,
	}, true, nil
}
