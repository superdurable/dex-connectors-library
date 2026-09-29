// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stripe

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
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

type checkoutSessionTriggerSource struct {
	client        *Client
	connection    sdkgo.ConnectionRef
	configuration CheckoutSessionUpdatedTriggerConfiguration
	deliveries    chan sdkgo.TriggerEvent[CheckoutSessionEvent]
	active        bool
	mu            sync.Mutex
}

type checkoutSessionRouteRegistry struct {
	mu     sync.RWMutex
	routes map[*checkoutSessionTriggerSource]sdkgo.TriggerTarget[CheckoutSessionEvent]
}

func newCheckoutSessionRouteRegistry() *checkoutSessionRouteRegistry {
	return &checkoutSessionRouteRegistry{routes: make(map[*checkoutSessionTriggerSource]sdkgo.TriggerTarget[CheckoutSessionEvent])}
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
	return &checkoutSessionTriggerSource{
		client: client, connection: connection, configuration: configuration,
		deliveries: make(chan sdkgo.TriggerEvent[CheckoutSessionEvent], 64),
	}
}

// Run makes this binding available to the client's webhook handler until cancellation.
func (source *checkoutSessionTriggerSource) Run(ctx context.Context, target sdkgo.TriggerTarget[CheckoutSessionEvent]) error {
	if target == nil {
		return fmt.Errorf("Stripe Checkout Session trigger target is required")
	}
	source.mu.Lock()
	if source.active {
		source.mu.Unlock()
		return fmt.Errorf("Stripe Checkout Session trigger source is already running")
	}
	source.active = true
	source.mu.Unlock()
	source.client.checkoutSessionRoutes.activate(source, target)
	defer func() {
		source.client.checkoutSessionRoutes.deactivate(source)
		source.mu.Lock()
		source.active = false
		source.mu.Unlock()
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event := <-source.deliveries:
			if err := sdkgo.DeliverTrigger(ctx, target, event); err != nil {
				return err
			}
		}
	}
}

func (registry *checkoutSessionRouteRegistry) activate(
	source *checkoutSessionTriggerSource,
	target sdkgo.TriggerTarget[CheckoutSessionEvent],
) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.routes[source] = target
}

func (registry *checkoutSessionRouteRegistry) deactivate(source *checkoutSessionTriggerSource) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	delete(registry.routes, source)
}

func (registry *checkoutSessionRouteRegistry) dispatch(
	ctx context.Context,
	event sdkgo.TriggerEvent[CheckoutSessionEvent],
) (bool, bool, error) {
	registry.mu.RLock()
	type route struct {
		source *checkoutSessionTriggerSource
		target sdkgo.TriggerTarget[CheckoutSessionEvent]
	}
	routes := make([]route, 0, len(registry.routes))
	for source, target := range registry.routes {
		routes = append(routes, route{source: source, target: target})
	}
	registry.mu.RUnlock()
	if len(routes) == 0 {
		return false, false, nil
	}
	matched := false
	deliveries := make([]route, 0, len(routes))
	for _, route := range routes {
		if !route.source.configuration.accepts(event.Payload.Type) {
			continue
		}
		matched = true
		if err := sdkgo.PrepareTriggerDelivery(ctx, route.target, event); err != nil {
			return true, true, err
		}
		deliveries = append(deliveries, route)
	}
	for _, delivery := range deliveries {
		select {
		case <-ctx.Done():
			return true, matched, ctx.Err()
		case delivery.source.deliveries <- event:
		default:
			return true, matched, fmt.Errorf("Stripe Checkout Session trigger delivery queue is full")
		}
	}
	return true, matched, nil
}

// CheckoutSessionWebhookHandler returns an HTTP handler for the configured Stripe webhook endpoint.
// The same Connection must also be used to construct and run the trigger binding.
func (connection Connection) CheckoutSessionWebhookHandler() (http.Handler, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	return &checkoutSessionWebhookHandler{connection: connection}, nil
}

type checkoutSessionWebhookHandler struct {
	connection Connection
}

// ServeHTTP verifies and dispatches one Stripe webhook request.
func (handler *checkoutSessionWebhookHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	contents, err := io.ReadAll(io.LimitReader(request.Body, handler.connection.client.webhookMaxBodyBytes+1))
	if err != nil {
		http.Error(response, "invalid request body", http.StatusBadRequest)
		return
	}
	if int64(len(contents)) > handler.connection.client.webhookMaxBodyBytes {
		http.Error(response, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	call := stripeWebhookCredentialCall(handler.connection.reference, contents)
	credentials, err := resolveWebhookCredentials(
		request.Context(), handler.connection.client.credentials, call,
	)
	if err != nil || credentials.WebhookSecret.Reveal() == "" {
		http.Error(response, "webhook temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := verifyWebhookSignature(
		contents,
		request.Header.Get(stripeSignatureHeader),
		credentials.WebhookSecret.Reveal(),
		handler.connection.client.now(),
		handler.connection.client.webhookTolerance,
	); err != nil {
		http.Error(response, "invalid webhook signature", http.StatusBadRequest)
		return
	}
	event, relevant, err := decodeWebhookEvent(contents)
	if err != nil {
		http.Error(response, "invalid webhook event", http.StatusBadRequest)
		return
	}
	if !relevant {
		response.WriteHeader(http.StatusOK)
		return
	}
	hadActiveRoutes, _, err := handler.connection.client.checkoutSessionRoutes.dispatch(request.Context(), event)
	if !hadActiveRoutes || err != nil {
		http.Error(response, "webhook delivery temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	response.WriteHeader(http.StatusOK)
}

func stripeWebhookCredentialCall(connection sdkgo.ConnectionRef, contents []byte) sdkgo.Call {
	digest := sha256.New()
	_, _ = digest.Write([]byte(ConnectorID))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(connection.Provider))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(connection.Name))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(contents)
	identity := digest.Sum(nil)[:16]
	identity[6] = (identity[6] & 0x0f) | 0x50
	identity[8] = (identity[8] & 0x3f) | 0x80
	encodedIdentity := hex.EncodeToString(identity)
	return sdkgo.Call{
		ID: sdkgo.CallID(encodedIdentity[0:8] + "-" + encodedIdentity[8:12] + "-" +
			encodedIdentity[12:16] + "-" + encodedIdentity[16:20] + "-" + encodedIdentity[20:32]),
		Connection: connection,
		Operation: sdkgo.OperationRef{
			ConnectorID: ConnectorID,
			OperationID: "checkoutSessionUpdated",
		},
	}
}

func resolveWebhookCredentials(
	ctx context.Context,
	provider sdkgo.CredentialProvider[Credentials],
	call sdkgo.Call,
) (Credentials, error) {
	if contextProvider, ok := provider.(sdkgo.ContextCredentialProvider[Credentials]); ok {
		return contextProvider.ResolveContext(ctx, call)
	}
	return provider.Resolve(call)
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
	handler http.Handler
	runners []sdkgo.TriggerRunner
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
	runtime := &CheckoutSessionWebhookRuntime{handler: handler}
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
		runtime.runners = append(runtime.runners, NewCheckoutSessionUpdatedTrigger(CheckoutSessionUpdatedTriggerConfig{
			Connection: connection, ConnectionName: connectionName, BindingName: bindingName,
			Configuration: configuration, Target: durableTarget,
		}))
	}
	return runtime, nil
}

// ServeHTTP receives the shared signed Stripe webhook endpoint.
func (runtime *CheckoutSessionWebhookRuntime) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	runtime.handler.ServeHTTP(response, request)
}

// Run activates every configured trigger route until cancellation or a runner failure.
func (runtime *CheckoutSessionWebhookRuntime) Run(ctx context.Context) error {
	if runtime == nil || len(runtime.runners) == 0 {
		return fmt.Errorf("Stripe Checkout Session webhook runtime is not configured")
	}
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, len(runtime.runners))
	for _, runner := range runtime.runners {
		go func(active sdkgo.TriggerRunner) { results <- active.Run(runContext) }(runner)
	}
	first := <-results
	cancel()
	for range len(runtime.runners) - 1 {
		<-results
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(first, context.Canceled) {
		return nil
	}
	return first
}
