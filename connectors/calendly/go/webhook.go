// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

// calendlyWebhookSignatureHeader carries t=<unix seconds>,v1=<hex HMAC-SHA256 of "t.body">.
const calendlyWebhookSignatureHeader = "Calendly-Webhook-Signature"

var errCalendlySignatureInvalid = errors.New("Calendly-Webhook-Signature is missing, malformed, stale, or does not match")

// InviteeEventReceivedTriggerConfiguration filters the Calendly webhook events one binding records. The
// zero value records every invitee.created and invitee.canceled event. It reduces what a binding stores;
// the application's TriggerFilter remains its admission rule.
type InviteeEventReceivedTriggerConfiguration struct {
	// Events lists invitee.created and invitee.canceled; empty accepts both.
	Events []string `json:"events,omitempty"`
	// EventTypeURI accepts only invitees of this Calendly event type, such as the eventTypePicker unit's
	// eventTypeUri; blank accepts every event type.
	EventTypeURI string `json:"eventTypeUri,omitempty"`
}

// InviteeEvent is one verified invitee.created or invitee.canceled webhook. The Trigger event ID is the
// webhook event followed by the scheduled event and invitee IDs from the invitee's stable URI, such as
// invitee.created:GBGBDCAADAEDCRZ2:AAAAAAAAAAAAAAAA, so a redelivery of one webhook keeps its ID.
type InviteeEvent struct {
	// Event is invitee.created or invitee.canceled.
	Event string `json:"event"`
	// CreatedAt is when Calendly created the webhook event.
	CreatedAt time.Time `json:"createdAt"`
	// Invitee is the invitee who booked or canceled. A reschedule sends invitee.canceled for the old
	// invitee, with Rescheduled set, and invitee.created for the new one.
	Invitee Invitee `json:"invitee"`
	// ScheduledEvent is the scheduled event as the webhook described it.
	ScheduledEvent ScheduledEvent `json:"scheduledEvent"`
}

type calendlyWebhookEnvelope struct {
	Event     string          `json:"event"`
	CreatedAt string          `json:"created_at"`
	Payload   json.RawMessage `json:"payload"`
}

type calendlyInviteeWebhookPayload struct {
	calendlyInvitee
	ScheduledEvent *calendlyScheduledEvent `json:"scheduled_event"`
}

// Validate checks that Events holds distinct supported webhook events and EventTypeURI is an event type URI.
func (configuration InviteeEventReceivedTriggerConfiguration) Validate() error {
	if !areSupportedWebhookEvents(configuration.Events) {
		return fmt.Errorf("inviteeEventReceived events must be distinct values from invitee.created and invitee.canceled")
	}
	if configuration.EventTypeURI != "" {
		if _, err := parseEventTypeURI(configuration.EventTypeURI); err != nil {
			return err
		}
	}
	return nil
}

// acceptsEvent applies the binding's event and event type filters.
func (configuration InviteeEventReceivedTriggerConfiguration) acceptsEvent(event sdkgo.TriggerEvent[InviteeEvent]) bool {
	if len(configuration.Events) > 0 && !slices.Contains(configuration.Events, event.Payload.Event) {
		return false
	}
	return configuration.EventTypeURI == "" || configuration.EventTypeURI == event.Payload.ScheduledEvent.EventTypeURI
}

// InviteeEventReceivedWebhookHandler returns the connection's webhook endpoint for an application to mount at
// the public HTTPS callback URL of its Calendly webhook subscription. Every inviteeEventReceived Trigger built
// from this Connection feeds from it, and it answers 503 while none of them runs, so Calendly retries.
func (connection Connection) InviteeEventReceivedWebhookHandler() (http.Handler, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	return connection.client.inviteeEventReceivedWebhookEndpoint(connection.reference)
}

func (client *Client) inviteeEventReceivedTriggerSource(
	connection sdkgo.ConnectionRef,
	configuration InviteeEventReceivedTriggerConfiguration,
) sdkgo.TriggerSource[InviteeEvent] {
	if err := configuration.Validate(); err != nil {
		panic(err)
	}
	endpoint, err := client.inviteeEventReceivedWebhookEndpoint(connection)
	if err != nil {
		panic(err)
	}
	return endpoint.NewSource(configuration.acceptsEvent)
}

// inviteeEventReceivedWebhookEndpoint returns the connection's shared endpoint, creating it on first use.
func (client *Client) inviteeEventReceivedWebhookEndpoint(
	connection sdkgo.ConnectionRef,
) (*webhooktrigger.Endpoint[Credentials, InviteeEvent], error) {
	client.inviteeEventEndpointsMu.Lock()
	defer client.inviteeEventEndpointsMu.Unlock()
	if endpoint, isFound := client.inviteeEventEndpoints[connection]; isFound {
		return endpoint, nil
	}
	endpoint, err := webhooktrigger.NewEndpoint(webhooktrigger.EndpointConfig[Credentials, InviteeEvent]{
		ConnectorID: ConnectorID, TriggerName: InviteeEventReceivedTriggerDefinition.Trigger.TriggerName,
		Connection: connection, Credentials: webhookCredentialProvider{client: client}, MaxBodyBytes: client.webhookMaxBodyBytes,
		VerifyRequest: client.verifyInviteeWebhookRequest, DecodeEvent: decodeInviteeWebhookRequest,
		Now: client.now, Logger: client.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("Calendly webhook endpoint: %w", err)
	}
	client.inviteeEventEndpoints[connection] = endpoint
	return endpoint, nil
}

// verifyInviteeWebhookRequest answers 503 without a signing key, so Calendly keeps retrying the delivery.
func (client *Client) verifyInviteeWebhookRequest(request webhooktrigger.Request, credentials Credentials) error {
	signingKey := credentials.WebhookSigningKey.Reveal()
	if signingKey == "" {
		return webhooktrigger.ErrVerificationUnavailable
	}
	return verifyCalendlyWebhookSignature(request.Body, request.Header.Get(calendlyWebhookSignatureHeader), signingKey,
		request.ReceivedAt, client.webhookSignatureTolerance)
}

// verifyCalendlyWebhookSignature requires a fresh t= and a v1= equal to hex HMAC-SHA256("t.body").
func verifyCalendlyWebhookSignature(body []byte, header string, signingKey string, now time.Time, tolerance time.Duration) error {
	var timestampText string
	var signatures [][]byte
	for _, component := range strings.Split(header, ",") {
		key, value, isCut := strings.Cut(strings.TrimSpace(component), "=")
		if !isCut {
			continue
		}
		switch key {
		case "t":
			timestampText = value
		case "v1":
			if decoded, err := hex.DecodeString(value); err == nil && len(decoded) == sha256.Size {
				signatures = append(signatures, decoded)
			}
		}
	}
	timestamp, err := strconv.ParseInt(timestampText, 10, 64)
	if err != nil || timestamp <= 0 || len(signatures) == 0 {
		return errCalendlySignatureInvalid
	}
	if age := now.Sub(time.Unix(timestamp, 0)); age > tolerance || age < -tolerance {
		return errCalendlySignatureInvalid
	}
	mac := hmac.New(sha256.New, []byte(signingKey))
	_, _ = mac.Write([]byte(timestampText + "."))
	_, _ = mac.Write(body)
	expected := mac.Sum(nil)
	for _, signature := range signatures {
		if hmac.Equal(signature, expected) {
			return nil
		}
	}
	return errCalendlySignatureInvalid
}

// decodeInviteeWebhookRequest decodes a verified delivery. Another webhook event, such as
// routing_form_submission.created, is acknowledged without a record.
func decodeInviteeWebhookRequest(request webhooktrigger.Request) (sdkgo.TriggerEvent[InviteeEvent], bool, error) {
	var envelope calendlyWebhookEnvelope
	if err := json.Unmarshal(request.Body, &envelope); err != nil || envelope.Event == "" || len(envelope.Payload) == 0 {
		return sdkgo.TriggerEvent[InviteeEvent]{}, false, errCalendlyResponseMalformed
	}
	if envelope.Event != WebhookEventInviteeCreated && envelope.Event != WebhookEventInviteeCanceled {
		return sdkgo.TriggerEvent[InviteeEvent]{}, false, nil
	}
	createdAt, err := parseCalendlyTimestamp(envelope.CreatedAt)
	if err != nil {
		return sdkgo.TriggerEvent[InviteeEvent]{}, false, err
	}
	var payload calendlyInviteeWebhookPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil || payload.ScheduledEvent == nil {
		return sdkgo.TriggerEvent[InviteeEvent]{}, false, errCalendlyResponseMalformed
	}
	invitee, err := payload.calendlyInvitee.convert()
	if err != nil {
		return sdkgo.TriggerEvent[InviteeEvent]{}, false, err
	}
	scheduledEvent, err := payload.ScheduledEvent.convert()
	if err != nil || scheduledEvent.URI != invitee.ScheduledEventURI {
		return sdkgo.TriggerEvent[InviteeEvent]{}, false, errCalendlyResponseMalformed
	}
	eventID, inviteeID, err := parseInviteeURI(invitee.URI)
	if err != nil {
		return sdkgo.TriggerEvent[InviteeEvent]{}, false, err
	}
	return sdkgo.TriggerEvent[InviteeEvent]{
		ID: envelope.Event + ":" + eventID + ":" + inviteeID, OccurredAt: createdAt,
		Payload: InviteeEvent{Event: envelope.Event, CreatedAt: createdAt, Invitee: invitee, ScheduledEvent: scheduledEvent},
	}, true, nil
}
