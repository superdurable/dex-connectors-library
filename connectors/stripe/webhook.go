// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stripe

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

const stripeSignatureHeader = "Stripe-Signature"

var supportedCheckoutSessionEventTypes = map[string]bool{
	"checkout.session.completed":               true,
	"checkout.session.async_payment_succeeded": true,
	"checkout.session.async_payment_failed":    true,
	"checkout.session.expired":                 true,
}

// CheckoutSessionUpdatedTriggerConfiguration selects the Stripe event types accepted by one binding.
// An empty list accepts every supported Checkout Session event type.
type CheckoutSessionUpdatedTriggerConfiguration struct {
	// EventTypes limits the binding to supported Stripe event types; empty accepts all supported types.
	EventTypes []string `json:"eventTypes,omitempty"`
}

// CheckoutSessionEvent is one verified and normalized Stripe Checkout Session event.
type CheckoutSessionEvent struct {
	// Type is the verified Stripe event type.
	Type string `json:"type"`
	// Session is the normalized Checkout Session snapshot carried by the event.
	Session CheckoutSession `json:"session"`
}

type stripeWebhookEvent struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Type    string `json:"type"`
	Created int64  `json:"created"`
	Data    struct {
		Object json.RawMessage `json:"object"`
	} `json:"data"`
}

// Validate checks that every configured Stripe event type is supported and unique.
func (configuration CheckoutSessionUpdatedTriggerConfiguration) Validate() error {
	seen := make(map[string]bool, len(configuration.EventTypes))
	for _, eventType := range configuration.EventTypes {
		if !supportedCheckoutSessionEventTypes[eventType] {
			return fmt.Errorf("Stripe Checkout Session event type %q is not supported", eventType)
		}
		if seen[eventType] {
			return fmt.Errorf("Stripe Checkout Session event types must be unique")
		}
		seen[eventType] = true
	}
	return nil
}

func (configuration CheckoutSessionUpdatedTriggerConfiguration) accepts(eventType string) bool {
	if len(configuration.EventTypes) == 0 {
		return supportedCheckoutSessionEventTypes[eventType]
	}
	for _, configured := range configuration.EventTypes {
		if configured == eventType {
			return true
		}
	}
	return false
}

func (client *Client) checkoutSessionUpdatedTriggerSource(
	connection sdkgo.ConnectionRef,
	configuration CheckoutSessionUpdatedTriggerConfiguration,
) sdkgo.TriggerSource[CheckoutSessionEvent] {
	if err := configuration.Validate(); err != nil {
		panic(err)
	}
	endpoint, err := client.checkoutSessionWebhookEndpoint(connection)
	if err != nil {
		panic(err)
	}
	return endpoint.NewSource(func(event sdkgo.TriggerEvent[CheckoutSessionEvent]) bool {
		return configuration.accepts(event.Payload.Type)
	})
}

// checkoutSessionWebhookEndpoint returns the connection's shared endpoint, creating it on first use.
func (client *Client) checkoutSessionWebhookEndpoint(
	connection sdkgo.ConnectionRef,
) (*webhooktrigger.Endpoint[Credentials, CheckoutSessionEvent], error) {
	client.checkoutSessionEndpointsMu.Lock()
	defer client.checkoutSessionEndpointsMu.Unlock()
	if endpoint, found := client.checkoutSessionEndpoints[connection]; found {
		return endpoint, nil
	}
	endpoint, err := webhooktrigger.NewEndpoint(webhooktrigger.EndpointConfig[Credentials, CheckoutSessionEvent]{
		ConnectorID: ConnectorID, TriggerName: CheckoutSessionUpdatedTriggerDefinition.Trigger.TriggerName,
		Connection: connection, Credentials: client.credentials, MaxBodyBytes: client.webhookMaxBodyBytes,
		VerifyRequest: func(request webhooktrigger.Request, credentials Credentials) error {
			if credentials.WebhookSecret.Reveal() == "" {
				return webhooktrigger.ErrVerificationUnavailable
			}
			return verifyWebhookSignature(
				request.Body, request.Header.Get(stripeSignatureHeader), credentials.WebhookSecret.Reveal(),
				request.ReceivedAt, client.webhookTolerance,
			)
		},
		DecodeEvent: func(request webhooktrigger.Request) (sdkgo.TriggerEvent[CheckoutSessionEvent], bool, error) {
			return decodeWebhookEvent(request.Body)
		},
		Now: client.now,
	})
	if err != nil {
		return nil, fmt.Errorf("Stripe webhook endpoint: %w", err)
	}
	client.checkoutSessionEndpoints[connection] = endpoint
	return endpoint, nil
}

// CheckoutSessionWebhookHandler returns an HTTP handler for the configured Stripe webhook endpoint.
// The same Connection must also be used to construct and run the trigger binding.
func (connection Connection) CheckoutSessionWebhookHandler() (http.Handler, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	return connection.client.checkoutSessionWebhookEndpoint(connection.reference)
}

func verifyWebhookSignature(contents []byte, header string, secret string, now time.Time, tolerance time.Duration) error {
	var timestamp int64
	var signatures [][]byte
	for _, component := range strings.Split(header, ",") {
		key, value, found := strings.Cut(strings.TrimSpace(component), "=")
		if !found {
			continue
		}
		switch key {
		case "t":
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid Stripe signature timestamp")
			}
			timestamp = parsed
		case "v1":
			decoded, err := hex.DecodeString(value)
			if err == nil {
				signatures = append(signatures, decoded)
			}
		}
	}
	if timestamp == 0 || len(signatures) == 0 || secret == "" {
		return fmt.Errorf("Stripe signature is incomplete")
	}
	delta := now.Sub(time.Unix(timestamp, 0))
	if delta < -tolerance || delta > tolerance {
		return fmt.Errorf("Stripe signature timestamp is outside the tolerance")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%d.", timestamp)
	_, _ = mac.Write(contents)
	expected := mac.Sum(nil)
	for _, signature := range signatures {
		if hmac.Equal(signature, expected) {
			return nil
		}
	}
	return fmt.Errorf("Stripe signature does not match")
}

func decodeWebhookEvent(contents []byte) (sdkgo.TriggerEvent[CheckoutSessionEvent], bool, error) {
	var envelope stripeWebhookEvent
	if err := jsonUnmarshal(contents, &envelope); err != nil {
		return sdkgo.TriggerEvent[CheckoutSessionEvent]{}, false, err
	}
	if envelope.Object != "event" || envelope.ID == "" || envelope.Created <= 0 || envelope.Type == "" {
		return sdkgo.TriggerEvent[CheckoutSessionEvent]{}, false, fmt.Errorf("Stripe event envelope is invalid")
	}
	if !supportedCheckoutSessionEventTypes[envelope.Type] {
		return sdkgo.TriggerEvent[CheckoutSessionEvent]{}, false, nil
	}
	session, err := decodeCheckoutSession(envelope.Data.Object)
	if err != nil {
		return sdkgo.TriggerEvent[CheckoutSessionEvent]{}, false, err
	}
	return sdkgo.TriggerEvent[CheckoutSessionEvent]{
		ID: envelope.ID, OccurredAt: time.Unix(envelope.Created, 0).UTC(),
		Payload: CheckoutSessionEvent{Type: envelope.Type, Session: session},
	}, true, nil
}

// LocalCheckoutSessionUpdatedTriggerRoute binds one stored trigger configuration to its application target.
type LocalCheckoutSessionUpdatedTriggerRoute struct {
	// BindingName names the stored trigger binding.
	BindingName string
	// Target receives verified events through the binding's durable local inbox.
	Target sdkgo.TriggerTarget[CheckoutSessionEvent]
}

// CheckoutSessionWebhookRuntime runs one or more bindings and serves their shared Stripe webhook endpoint.
type CheckoutSessionWebhookRuntime struct {
	endpointRunner *webhooktrigger.EndpointRunner
}

// NewLocalCheckoutSessionWebhookRuntime loads local connection and binding configuration and creates a
// shared webhook runtime. Each target receives a durable inbox before Stripe is acknowledged.
func NewLocalCheckoutSessionWebhookRuntime(
	store *localconfig.Store,
	connectionName string,
	routes []LocalCheckoutSessionUpdatedTriggerRoute,
	options ...Option,
) (*CheckoutSessionWebhookRuntime, error) {
	if store == nil {
		return nil, fmt.Errorf("local connector configuration store is required")
	}
	if len(routes) == 0 {
		return nil, fmt.Errorf("Stripe Checkout Session trigger routes are required")
	}
	connection, err := NewLocalConnection(store, connectionName, options...)
	if err != nil {
		return nil, err
	}
	handler, err := connection.CheckoutSessionWebhookHandler()
	if err != nil {
		return nil, err
	}
	runners := make([]sdkgo.TriggerRunner, 0, len(routes))
	bindings := make(map[string]bool, len(routes))
	for _, route := range routes {
		bindingName := strings.TrimSpace(route.BindingName)
		if bindingName == "" || route.Target == nil {
			return nil, fmt.Errorf("Stripe Checkout Session trigger binding name and target are required")
		}
		if bindings[bindingName] {
			return nil, fmt.Errorf("Stripe Checkout Session trigger binding name %q is duplicated", bindingName)
		}
		bindings[bindingName] = true
		var configuration CheckoutSessionUpdatedTriggerConfiguration
		if err := store.DecodeTriggerConfiguration(ConnectorID, connectionName, "checkoutSessionUpdated", bindingName, &configuration); err != nil {
			return nil, err
		}
		durableTarget, err := localconfig.NewDurableTriggerTarget(
			store, ConnectorID, connectionName, "checkoutSessionUpdated", bindingName, route.Target,
		)
		if err != nil {
			return nil, err
		}
		runners = append(runners, NewCheckoutSessionUpdatedTrigger(CheckoutSessionUpdatedTriggerConfig{
			Connection: connection, ConnectionName: connectionName, BindingName: bindingName,
			Configuration: configuration, Target: durableTarget,
		}))
	}
	endpointRunner, err := webhooktrigger.NewEndpointRunner(handler, runners...)
	if err != nil {
		return nil, err
	}
	return &CheckoutSessionWebhookRuntime{endpointRunner: endpointRunner}, nil
}

// ServeHTTP receives the shared signed Stripe webhook endpoint.
func (runtime *CheckoutSessionWebhookRuntime) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	runtime.endpointRunner.ServeHTTP(response, request)
}

// Run activates every configured trigger route until cancellation or a runner failure.
func (runtime *CheckoutSessionWebhookRuntime) Run(ctx context.Context) error {
	if runtime == nil || runtime.endpointRunner == nil {
		return fmt.Errorf("Stripe Checkout Session webhook runtime is not configured")
	}
	return runtime.endpointRunner.Run(ctx)
}
