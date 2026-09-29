// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stripe_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/stripe"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestCheckoutSessionWebhookVerifiesPersistsThenDelivers(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	client, err := stripe.New(stripe.Config{Endpoint: "http://127.0.0.1:1"}, sdkgo.StaticCredentialProvider[stripe.Credentials]{
		stripeConnection: {SecretKey: sdkgo.NewSecretString("sk_test_example"), WebhookSecret: sdkgo.NewSecretString("whsec_example")},
	}, stripe.WithClock(func() time.Time { return now }))
	require.NoError(t, err)
	connection, err := stripe.NewConnection(client, stripeConnection)
	require.NoError(t, err)
	target := &recordingCheckoutTarget{}
	runner := stripe.NewCheckoutSessionUpdatedTrigger(stripe.CheckoutSessionUpdatedTriggerConfig{
		Connection: connection, ConnectionName: "payments", BindingName: "registration-payments",
		Configuration: stripe.CheckoutSessionUpdatedTriggerConfiguration{EventTypes: []string{"checkout.session.async_payment_succeeded"}},
		Target:        target,
	})
	handler, err := connection.CheckoutSessionWebhookHandler()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	runFinished := make(chan error, 1)
	go func() { runFinished <- runner.Run(ctx) }()
	defer func() {
		cancel()
		require.ErrorIs(t, <-runFinished, context.Canceled)
	}()

	body := `{"id":"evt_123","object":"event","type":"checkout.session.async_payment_succeeded","created":1700000000,"data":{"object":{"id":"cs_test_123","object":"checkout.session","client_reference_id":"registration-123","payment_status":"paid","status":"complete","payment_intent":"pi_123","currency":"usd","amount_total":7500,"metadata":{"registration_id":"registration-123"}}}}`
	status := 0
	require.Eventually(t, func() bool {
		status = serveSignedWebhook(handler, body, "whsec_example", now)
		return status == http.StatusOK
	}, time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool { return len(target.calls()) == 2 }, time.Second, 10*time.Millisecond)
	require.Equal(t, []string{"prepare:evt_123", "handle:evt_123"}, target.calls())
	require.Equal(t, "registration-123", target.event().Payload.Session.ClientReferenceID)
	require.Equal(t, "pi_123", target.event().Payload.Session.PaymentIntentID)

	ignored := strings.Replace(body, "checkout.session.async_payment_succeeded", "checkout.session.completed", 1)
	require.Equal(t, http.StatusOK, serveSignedWebhook(handler, ignored, "whsec_example", now))
	require.Equal(t, []string{"prepare:evt_123", "handle:evt_123"}, target.calls())
}

func TestCheckoutSessionWebhookAcknowledgesAfterPersistenceWithoutWaitingForTarget(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	client, err := stripe.New(stripe.Config{Endpoint: "http://127.0.0.1:1"}, sdkgo.StaticCredentialProvider[stripe.Credentials]{
		stripeConnection: {SecretKey: sdkgo.NewSecretString("sk_test_example"), WebhookSecret: sdkgo.NewSecretString("whsec_example")},
	}, stripe.WithClock(func() time.Time { return now }))
	require.NoError(t, err)
	connection, err := stripe.NewConnection(client, stripeConnection)
	require.NoError(t, err)
	target := newBlockingCheckoutTarget()
	runner := stripe.NewCheckoutSessionUpdatedTrigger(stripe.CheckoutSessionUpdatedTriggerConfig{
		Connection: connection, ConnectionName: "payments", BindingName: "registration-payments",
		Configuration: stripe.CheckoutSessionUpdatedTriggerConfiguration{}, Target: target,
	})
	handler, err := connection.CheckoutSessionWebhookHandler()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	runFinished := make(chan error, 1)
	go func() { runFinished <- runner.Run(ctx) }()
	defer func() {
		close(target.release)
		cancel()
		require.ErrorIs(t, <-runFinished, context.Canceled)
	}()
	body := `{"id":"evt_ack","object":"event","type":"checkout.session.completed","created":1700000000,"data":{"object":{"id":"cs_test_ack","object":"checkout.session"}}}`
	status := 0
	require.Eventually(t, func() bool {
		status = serveSignedWebhook(handler, body, "whsec_example", now)
		return status == http.StatusOK
	}, time.Second, 10*time.Millisecond)
	select {
	case <-target.prepared:
	default:
		t.Fatal("webhook was acknowledged before durable preparation")
	}
	select {
	case <-target.handling:
	case <-time.After(time.Second):
		t.Fatal("queued webhook was not delivered")
	}
}

func TestCheckoutSessionWebhookRejectsInvalidSignatureAgeAndBody(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	client, err := stripe.New(stripe.Config{Endpoint: "http://127.0.0.1:1", WebhookMaxBodyBytes: 32}, sdkgo.StaticCredentialProvider[stripe.Credentials]{
		stripeConnection: {SecretKey: sdkgo.NewSecretString("sk_test_example"), WebhookSecret: sdkgo.NewSecretString("whsec_example")},
	}, stripe.WithClock(func() time.Time { return now }))
	require.NoError(t, err)
	connection, err := stripe.NewConnection(client, stripeConnection)
	require.NoError(t, err)
	handler, err := connection.CheckoutSessionWebhookHandler()
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(`{"small":true}`))
	request.Header.Set("Stripe-Signature", webhookSignature(`{"small":true}`, "wrong", now))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)

	request = httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(strings.Repeat("x", 33)))
	request.Header.Set("Stripe-Signature", webhookSignature(strings.Repeat("x", 33), "whsec_example", now))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusRequestEntityTooLarge, response.Code)

	request = httptest.NewRequest(http.MethodGet, "/webhooks/stripe", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusMethodNotAllowed, response.Code)
}

func TestCheckoutSessionWebhookReturnsRetryableStatusUntilTriggerRuns(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	client, err := stripe.New(stripe.Config{Endpoint: "http://127.0.0.1:1"}, sdkgo.StaticCredentialProvider[stripe.Credentials]{
		stripeConnection: {SecretKey: sdkgo.NewSecretString("sk_test_example"), WebhookSecret: sdkgo.NewSecretString("whsec_example")},
	}, stripe.WithClock(func() time.Time { return now }))
	require.NoError(t, err)
	connection, err := stripe.NewConnection(client, stripeConnection)
	require.NoError(t, err)
	handler, err := connection.CheckoutSessionWebhookHandler()
	require.NoError(t, err)
	body := `{"id":"evt_123","object":"event","type":"checkout.session.completed","created":1700000000,"data":{"object":{"id":"cs_test_123","object":"checkout.session"}}}`
	require.Equal(t, http.StatusServiceUnavailable, serveSignedWebhook(handler, body, "whsec_example", now))
}

func TestCheckoutSessionWebhookResolvesTriggerScopedCredentials(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	provider := &recordingCredentialProvider{credentials: stripe.Credentials{
		SecretKey:     sdkgo.NewSecretString("sk_test_example"),
		WebhookSecret: sdkgo.NewSecretString("whsec_example"),
	}}
	client, err := stripe.New(
		stripe.Config{Endpoint: "http://127.0.0.1:1"},
		provider,
		stripe.WithClock(func() time.Time { return now }),
	)
	require.NoError(t, err)
	connection, err := stripe.NewConnection(client, stripeConnection)
	require.NoError(t, err)
	handler, err := connection.CheckoutSessionWebhookHandler()
	require.NoError(t, err)
	body := `{"id":"evt_123","object":"event","type":"checkout.session.completed","created":1700000000,"data":{"object":{"id":"cs_test_123","object":"checkout.session"}}}`

	require.Equal(t, http.StatusServiceUnavailable, serveSignedWebhook(handler, body, "whsec_example", now))
	require.Equal(t, http.StatusServiceUnavailable, serveSignedWebhook(handler, body, "whsec_example", now))

	calls, usedContext := provider.snapshot()
	require.True(t, usedContext)
	require.Len(t, calls, 2)
	require.NoError(t, calls[0].ID.Validate())
	require.Equal(t, calls[0].ID, calls[1].ID)
	require.Equal(t, stripe.ConnectorID, calls[0].Operation.ConnectorID)
	require.Equal(t, "checkoutSessionUpdated", calls[0].Operation.OperationID)
	require.Equal(t, stripeConnection, calls[0].Connection)
}

func TestCheckoutSessionTriggerConfigurationRejectsUnknownAndDuplicateEvents(t *testing.T) {
	require.Error(t, (stripe.CheckoutSessionUpdatedTriggerConfiguration{EventTypes: []string{"charge.succeeded"}}).Validate())
	require.Error(t, (stripe.CheckoutSessionUpdatedTriggerConfiguration{EventTypes: []string{"checkout.session.completed", "checkout.session.completed"}}).Validate())
	require.NoError(t, (stripe.CheckoutSessionUpdatedTriggerConfiguration{}).Validate())
}

type recordingCheckoutTarget struct {
	mu       sync.Mutex
	sequence []string
	received sdkgo.TriggerEvent[stripe.CheckoutSessionEvent]
}

type blockingCheckoutTarget struct {
	prepared chan struct{}
	handling chan struct{}
	release  chan struct{}
	prepare  sync.Once
	handle   sync.Once
}

type recordingCredentialProvider struct {
	mu          sync.Mutex
	credentials stripe.Credentials
	calls       []sdkgo.Call
	usedContext bool
}

func (provider *recordingCredentialProvider) Resolve(call sdkgo.Call) (stripe.Credentials, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.calls = append(provider.calls, call)
	return provider.credentials, nil
}

func (provider *recordingCredentialProvider) ResolveContext(
	_ context.Context,
	call sdkgo.Call,
) (stripe.Credentials, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.usedContext = true
	provider.calls = append(provider.calls, call)
	return provider.credentials, nil
}

func (provider *recordingCredentialProvider) snapshot() ([]sdkgo.Call, bool) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return append([]sdkgo.Call(nil), provider.calls...), provider.usedContext
}

func newBlockingCheckoutTarget() *blockingCheckoutTarget {
	return &blockingCheckoutTarget{prepared: make(chan struct{}), handling: make(chan struct{}), release: make(chan struct{})}
}

func (target *blockingCheckoutTarget) PrepareTrigger(context.Context, sdkgo.TriggerEvent[stripe.CheckoutSessionEvent]) error {
	target.prepare.Do(func() { close(target.prepared) })
	return nil
}

func (target *blockingCheckoutTarget) HandleTrigger(ctx context.Context, _ sdkgo.TriggerEvent[stripe.CheckoutSessionEvent]) error {
	target.handle.Do(func() { close(target.handling) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-target.release:
		return nil
	}
}

func (target *recordingCheckoutTarget) PrepareTrigger(_ context.Context, event sdkgo.TriggerEvent[stripe.CheckoutSessionEvent]) error {
	target.mu.Lock()
	defer target.mu.Unlock()
	target.sequence = append(target.sequence, "prepare:"+event.ID)
	return nil
}

func (target *recordingCheckoutTarget) HandleTrigger(_ context.Context, event sdkgo.TriggerEvent[stripe.CheckoutSessionEvent]) error {
	target.mu.Lock()
	defer target.mu.Unlock()
	target.sequence = append(target.sequence, "handle:"+event.ID)
	target.received = event
	return nil
}

func (target *recordingCheckoutTarget) calls() []string {
	target.mu.Lock()
	defer target.mu.Unlock()
	return append([]string(nil), target.sequence...)
}

func (target *recordingCheckoutTarget) event() sdkgo.TriggerEvent[stripe.CheckoutSessionEvent] {
	target.mu.Lock()
	defer target.mu.Unlock()
	return target.received
}

func serveSignedWebhook(handler http.Handler, body string, secret string, timestamp time.Time) int {
	request := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(body))
	request.Header.Set("Stripe-Signature", webhookSignature(body, secret, timestamp))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response.Code
}

func webhookSignature(body string, secret string, timestamp time.Time) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%d.%s", timestamp.Unix(), body)
	return fmt.Sprintf("t=%d,v1=%s", timestamp.Unix(), hex.EncodeToString(mac.Sum(nil)))
}
