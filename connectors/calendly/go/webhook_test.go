// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/calendly"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// webhookFixture is one connection whose inviteeEventReceived binding runs until the test ends.
type webhookFixture struct {
	handler http.Handler
	events  chan sdkgo.TriggerEvent[calendly.InviteeEvent]
}

func newWebhookFixture(
	t *testing.T, config calendly.Config, signingKey string, configuration calendly.InviteeEventReceivedTriggerConfiguration,
) *webhookFixture {
	t.Helper()
	client := newTestClient(t, nil, personalAccessTokenCredentials(signingKey), config)
	connection, err := calendly.NewConnection(client, testConnection)
	require.NoError(t, err)
	handler, err := connection.InviteeEventReceivedWebhookHandler()
	require.NoError(t, err)
	fixture := &webhookFixture{handler: handler, events: make(chan sdkgo.TriggerEvent[calendly.InviteeEvent], 16)}
	runner := calendly.NewInviteeEventReceivedTrigger(calendly.InviteeEventReceivedTriggerConfig{
		Connection: connection, ConnectionName: testConnection.Name, BindingName: "bookings", Configuration: configuration,
		Target: sdkgo.TriggerTargetFunc[calendly.InviteeEvent](func(_ context.Context, event sdkgo.TriggerEvent[calendly.InviteeEvent]) error {
			fixture.events <- event
			return nil
		}),
	})
	ctx, cancel := context.WithCancel(context.Background())
	runFinished := make(chan error, 1)
	go func() { runFinished <- runner.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.ErrorIs(t, <-runFinished, context.Canceled)
	})
	readiness := handler.(interface{ RunningSourceCount() int })
	require.Eventually(t, func() bool { return readiness.RunningSourceCount() == 1 }, 5*time.Second, time.Millisecond)
	return fixture
}

// deliver posts body with a Calendly-Webhook-Signature computed at signedAt; a blank key sends no header.
func (fixture *webhookFixture) deliver(t *testing.T, body string, signingKey string, signedAt time.Time) int {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/webhooks/calendly", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if signingKey != "" {
		request.Header.Set("Calendly-Webhook-Signature", calendlySignature(signingKey, signedAt, body))
	}
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	require.NotContains(t, response.Body.String(), sentinelSigningKey)
	return response.Code
}

func (fixture *webhookFixture) receiveEvent(t *testing.T) sdkgo.TriggerEvent[calendly.InviteeEvent] {
	t.Helper()
	select {
	case event := <-fixture.events:
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("the verified event was not delivered")
		return sdkgo.TriggerEvent[calendly.InviteeEvent]{}
	}
}

func (fixture *webhookFixture) requireNoEvent(t *testing.T) {
	t.Helper()
	select {
	case event := <-fixture.events:
		t.Fatalf("unexpected event %s", event.ID)
	case <-time.After(50 * time.Millisecond): // Delivery is asynchronous after the 200.
	}
}

// calendlySignature signs as Calendly documents: hex HMAC-SHA256 of "<t>.<body>" in "t=<t>,v1=<hex>".
func calendlySignature(signingKey string, signedAt time.Time, body string) string {
	timestamp := strconv.FormatInt(signedAt.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(signingKey))
	mac.Write([]byte(timestamp + "." + body))
	return "t=" + timestamp + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

func inviteeWebhookBody(event string, inviteeID string, eventTypeURI string) string {
	scheduledEvent := strings.Replace(scheduledEventJSON("EVENT0001", "active", testWindowStart), testEventTypeURI, eventTypeURI, 1)
	invitee := inviteeJSON("EVENT0001", inviteeID, map[string]string{"invitee.created": "active", "invitee.canceled": "canceled"}[event])
	return fmt.Sprintf(`{"event":%q,"created_at":"2026-09-30T11:59:00.000000Z","created_by":%q,"payload":%s,"scheduled_event":%s}}`,
		event, testUserURI, invitee[:len(invitee)-1], scheduledEvent)
}

func TestInviteeWebhookVerifiesTheSignatureAndDeliversTheInviteeWithItsStableID(t *testing.T) {
	fixture := newWebhookFixture(t, calendly.Config{}, sentinelSigningKey, calendly.InviteeEventReceivedTriggerConfiguration{})
	body := inviteeWebhookBody(calendly.WebhookEventInviteeCreated, "INVITEE01", testEventTypeURI)

	require.Equal(t, http.StatusOK, fixture.deliver(t, body, sentinelSigningKey, fixedNow))
	event := fixture.receiveEvent(t)
	require.Equal(t, "invitee.created:EVENT0001:INVITEE01", event.ID)
	require.Equal(t, time.Date(2026, time.September, 30, 11, 59, 0, 0, time.UTC), event.OccurredAt)
	require.Equal(t, calendly.WebhookEventInviteeCreated, event.Payload.Event)
	require.Equal(t, testEventURI+"/invitees/INVITEE01", event.Payload.Invitee.URI)
	require.Equal(t, "ada@example.com", event.Payload.Invitee.Email)
	require.Equal(t, testEventURI, event.Payload.ScheduledEvent.URI)
	require.Equal(t, testEventTypeURI, event.Payload.ScheduledEvent.EventTypeURI)
	requireSecretFree(t, event)

	require.Equal(t, http.StatusOK, fixture.deliver(t, body, sentinelSigningKey, fixedNow.Add(-time.Minute)), "a redelivery is re-signed")
	require.Equal(t, event.ID, fixture.receiveEvent(t).ID, "a redelivery keeps its event ID")
	canceled := inviteeWebhookBody(calendly.WebhookEventInviteeCanceled, "INVITEE01", testEventTypeURI)
	require.Equal(t, http.StatusOK, fixture.deliver(t, canceled, sentinelSigningKey, fixedNow))
	require.Equal(t, "invitee.canceled:EVENT0001:INVITEE01", fixture.receiveEvent(t).ID, "the cancellation has its own ID")
}

func TestInviteeWebhookRejectsForgedStaleAndUnsignedDeliveries(t *testing.T) {
	fixture := newWebhookFixture(t, calendly.Config{}, sentinelSigningKey, calendly.InviteeEventReceivedTriggerConfiguration{})
	body := inviteeWebhookBody(calendly.WebhookEventInviteeCreated, "INVITEE01", testEventTypeURI)
	for _, test := range []struct {
		name      string
		header    string
		body      string
		isForward bool
	}{
		{name: "wrong key", header: calendlySignature("another-key", fixedNow, body), body: body},
		{name: "tampered body", header: calendlySignature(sentinelSigningKey, fixedNow, body), body: strings.Replace(body, "ada@", "eve@", 1)},
		{name: "stale timestamp", header: calendlySignature(sentinelSigningKey, fixedNow.Add(-4*time.Minute), body), body: body},
		{name: "future timestamp", header: calendlySignature(sentinelSigningKey, fixedNow.Add(4*time.Minute), body), body: body},
		{name: "missing header", header: "", body: body},
		{name: "no v1", header: "t=" + strconv.FormatInt(fixedNow.Unix(), 10), body: body},
		{name: "no timestamp", header: "v1=" + strings.Repeat("ab", 32), body: body},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/webhooks/calendly", strings.NewReader(test.body))
			if test.header != "" {
				request.Header.Set("Calendly-Webhook-Signature", test.header)
			}
			response := httptest.NewRecorder()
			fixture.handler.ServeHTTP(response, request)
			require.Equal(t, http.StatusBadRequest, response.Code)
			require.NotContains(t, response.Body.String(), "Calendly-Webhook-Signature is")
		})
	}
	fixture.requireNoEvent(t)
	require.Equal(t, http.StatusOK, fixture.deliver(t, body, sentinelSigningKey, fixedNow.Add(-170*time.Second)), "within the three-minute default")
	fixture.receiveEvent(t)
}

func TestInviteeWebhookAnswers503WithoutASigningKeyAndAcknowledgesOtherEvents(t *testing.T) {
	unsigned := newWebhookFixture(t, calendly.Config{}, "", calendly.InviteeEventReceivedTriggerConfiguration{})
	body := inviteeWebhookBody(calendly.WebhookEventInviteeCreated, "INVITEE01", testEventTypeURI)
	require.Equal(t, http.StatusServiceUnavailable, unsigned.deliver(t, body, sentinelSigningKey, fixedNow), "Calendly keeps retrying")

	fixture := newWebhookFixture(t, calendly.Config{}, sentinelSigningKey, calendly.InviteeEventReceivedTriggerConfiguration{})
	routing := `{"event":"routing_form_submission.created","created_at":"2026-09-30T11:59:00.000000Z","payload":{"uri":"https://api.calendly.com/routing_form_submissions/X"}}`
	require.Equal(t, http.StatusOK, fixture.deliver(t, routing, sentinelSigningKey, fixedNow), "another event family is acknowledged")
	require.Equal(t, http.StatusBadRequest, fixture.deliver(t, `{"event":"invitee.created","payload":{}}`, sentinelSigningKey, fixedNow))
	require.Equal(t, http.StatusBadRequest, fixture.deliver(t, `not json`, sentinelSigningKey, fixedNow))
	fixture.requireNoEvent(t)

	small := newWebhookFixture(t, calendly.Config{WebhookMaxBodyBytes: 64}, sentinelSigningKey, calendly.InviteeEventReceivedTriggerConfiguration{})
	require.Equal(t, http.StatusRequestEntityTooLarge, small.deliver(t, body, sentinelSigningKey, fixedNow))
	response := httptest.NewRecorder()
	small.handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/webhooks/calendly", nil))
	require.Equal(t, http.StatusMethodNotAllowed, response.Code)
}

func TestInviteeWebhookBindingFiltersByEventAndEventType(t *testing.T) {
	const otherEventTypeURI = "https://api.calendly.com/event_types/TYPE0002"
	fixture := newWebhookFixture(t, calendly.Config{}, sentinelSigningKey, calendly.InviteeEventReceivedTriggerConfiguration{
		Events: []string{calendly.WebhookEventInviteeCreated}, EventTypeURI: testEventTypeURI,
	})
	for _, body := range []string{
		inviteeWebhookBody(calendly.WebhookEventInviteeCanceled, "INVITEE01", testEventTypeURI),
		inviteeWebhookBody(calendly.WebhookEventInviteeCreated, "INVITEE02", otherEventTypeURI),
	} {
		require.Equal(t, http.StatusOK, fixture.deliver(t, body, sentinelSigningKey, fixedNow), "a filtered event is still acknowledged")
	}
	require.Equal(t, http.StatusOK, fixture.deliver(t, inviteeWebhookBody(calendly.WebhookEventInviteeCreated, "INVITEE03", testEventTypeURI), sentinelSigningKey, fixedNow))
	require.Equal(t, "invitee.created:EVENT0001:INVITEE03", fixture.receiveEvent(t).ID)
	fixture.requireNoEvent(t)
}

func TestInviteeEventReceivedTriggerConfigurationValidates(t *testing.T) {
	for _, configuration := range []calendly.InviteeEventReceivedTriggerConfiguration{
		{Events: []string{"invitee_no_show.created"}},
		{Events: []string{"invitee.created", "invitee.created"}},
		{EventTypeURI: "https://api.calendly.com/users/USER0001"},
	} {
		require.Error(t, configuration.Validate(), configuration)
	}
	require.NoError(t, calendly.InviteeEventReceivedTriggerConfiguration{}.Validate())
	require.NoError(t, calendly.InviteeEventReceivedTriggerConfiguration{
		Events: []string{"invitee.created", "invitee.canceled"}, EventTypeURI: testEventTypeURI,
	}.Validate())
}
