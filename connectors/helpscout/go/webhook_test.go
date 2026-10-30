// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/helpscout"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// webhookFixture is one connection whose conversationEvent binding runs until the test ends.
type webhookFixture struct {
	handler http.Handler
	events  chan sdkgo.TriggerEvent[helpscout.ConversationEvent]
}

func newWebhookFixture(t *testing.T, config helpscout.Config, webhookSecret string, configuration helpscout.ConversationEventTriggerConfiguration) *webhookFixture {
	t.Helper()
	return startWebhookFixture(t, newTestClient(t, nil, staticCredentials(webhookSecret), config), configuration)
}

func startWebhookFixture(t *testing.T, client *helpscout.Client, configuration helpscout.ConversationEventTriggerConfiguration) *webhookFixture {
	t.Helper()
	connection, err := helpscout.NewConnection(client, testConnection)
	require.NoError(t, err)
	handler, err := connection.ConversationEventWebhookHandler()
	require.NoError(t, err)
	fixture := &webhookFixture{handler: handler, events: make(chan sdkgo.TriggerEvent[helpscout.ConversationEvent], 16)}
	runner := helpscout.NewConversationEventTrigger(helpscout.ConversationEventTriggerConfig{
		Connection: connection, ConnectionName: testConnection.Name, BindingName: "conversations", Configuration: configuration,
		Target: sdkgo.TriggerTargetFunc[helpscout.ConversationEvent](func(_ context.Context, event sdkgo.TriggerEvent[helpscout.ConversationEvent]) error {
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

// deliver posts body as Help Scout does, signed with secret; a blank secret sends no signature.
func (fixture *webhookFixture) deliver(t *testing.T, event string, body string, secret string) int {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/webhooks/helpscout", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if event != "" {
		request.Header.Set("X-HelpScout-Event", event)
	}
	if secret != "" {
		request.Header.Set("X-HelpScout-Signature", helpScoutSignature(secret, body))
	}
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	require.NotContains(t, response.Body.String(), sentinelWebhookSecret)
	return response.Code
}

func (fixture *webhookFixture) receiveEvent(t *testing.T) sdkgo.TriggerEvent[helpscout.ConversationEvent] {
	t.Helper()
	select {
	case event := <-fixture.events:
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("the verified event was not delivered")
		return sdkgo.TriggerEvent[helpscout.ConversationEvent]{}
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

// helpScoutSignature signs as Help Scout documents: base64 of the HMAC-SHA1 of the raw body.
func helpScoutSignature(secret string, body string) string {
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(body))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// TestSignatureMatchesHelpScoutsDocumentedExample uses the body, key, and signature that the Help Scout
// webhook documentation calls an actual signature of that data.
func TestSignatureMatchesHelpScoutsDocumentedExample(t *testing.T) {
	const documentedBody = `{"ticket":{"id":"1","number":"2"},"customer":{"id":"1","fname":"Jackie","lname":"Chan","email":"jackie.chan@somewhere.com","emails":["jackie.chan@somewhere.com"]}}`
	require.Equal(t, "I1KlvGppYqvFTJgJ9jezdQMDiyI=", helpScoutSignature("your secret key", documentedBody))
	fixture := newWebhookFixture(t, helpscout.Config{}, "your secret key", helpscout.ConversationEventTriggerConfiguration{})
	request := httptest.NewRequest(http.MethodPost, "/webhooks/helpscout", strings.NewReader(documentedBody))
	request.Header.Set("X-HelpScout-Event", "customer.created")
	request.Header.Set("X-HelpScout-Signature", "I1KlvGppYqvFTJgJ9jezdQMDiyI=")
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, "the documented signature verifies, and a customer event is acknowledged")
	fixture.requireNoEvent(t)
}

func TestConversationWebhookDeliversAVerifiedConversationWithAStableID(t *testing.T) {
	fixture := newWebhookFixture(t, helpscout.Config{}, sentinelWebhookSecret, helpscout.ConversationEventTriggerConfiguration{})
	body := conversationJSON(501, "active", 123, "billing")

	require.Equal(t, http.StatusOK, fixture.deliver(t, helpscout.WebhookEventConversationCreated, body, sentinelWebhookSecret))
	event := fixture.receiveEvent(t)
	require.Regexp(t, `^convo\.created:501:[0-9a-f]{32}$`, event.ID)
	require.Equal(t, fixedNow, event.OccurredAt, "Help Scout sends no event time, so it is the receipt time")
	require.Equal(t, helpscout.WebhookEventConversationCreated, event.Payload.Event)
	require.EqualValues(t, 501, event.Payload.Conversation.ID)
	require.EqualValues(t, 123, event.Payload.Conversation.MailboxID)
	require.Equal(t, "jane@acme.example.com", event.Payload.Conversation.PrimaryCustomer.Email)
	require.Equal(t, []string{"billing"}, event.Payload.Conversation.Tags)
	requireSecretFree(t, event)

	require.Equal(t, http.StatusOK, fixture.deliver(t, helpscout.WebhookEventConversationCreated, body, sentinelWebhookSecret))
	require.Equal(t, event.ID, fixture.receiveEvent(t).ID, "a redelivery of the same body keeps its ID")
	require.Equal(t, http.StatusOK, fixture.deliver(t, helpscout.WebhookEventConversationTagsUpdated, body, sentinelWebhookSecret))
	require.NotEqual(t, event.ID, fixture.receiveEvent(t).ID, "another event about the conversation has its own ID")
	changed := conversationJSON(501, "pending", 123, "billing")
	require.Equal(t, http.StatusOK, fixture.deliver(t, helpscout.WebhookEventConversationStatusUpdated, changed, sentinelWebhookSecret))
	require.Equal(t, helpscout.ConversationStatusPending, fixture.receiveEvent(t).Payload.Conversation.Status)
}

func TestConversationWebhookRejectsForgedAndMalformedDeliveries(t *testing.T) {
	fixture := newWebhookFixture(t, helpscout.Config{}, sentinelWebhookSecret, helpscout.ConversationEventTriggerConfiguration{})
	body := conversationJSON(501, "active", 123)
	for _, test := range []struct {
		name      string
		event     string
		signature string
		body      string
	}{
		{name: "wrong secret", event: helpscout.WebhookEventConversationCreated, signature: helpScoutSignature("another-secret", body), body: body},
		{name: "tampered body", event: helpscout.WebhookEventConversationCreated, signature: helpScoutSignature(sentinelWebhookSecret, body),
			body: strings.Replace(body, "jane@", "eve@", 1)},
		{name: "missing signature", event: helpscout.WebhookEventConversationCreated, body: body},
		{name: "hex signature", event: helpscout.WebhookEventConversationCreated, signature: strings.Repeat("ab", 20), body: body},
		{name: "missing event", signature: helpScoutSignature(sentinelWebhookSecret, body), body: body},
		{name: "notification payload", event: helpscout.WebhookEventConversationCreated,
			signature: helpScoutSignature(sentinelWebhookSecret, `{"url":"https://api.helpscout.net/v2/conversations/501"}`),
			body:      `{"url":"https://api.helpscout.net/v2/conversations/501"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/webhooks/helpscout", strings.NewReader(test.body))
			if test.event != "" {
				request.Header.Set("X-HelpScout-Event", test.event)
			}
			if test.signature != "" {
				request.Header.Set("X-HelpScout-Signature", test.signature)
			}
			response := httptest.NewRecorder()
			fixture.handler.ServeHTTP(response, request)
			require.Equal(t, http.StatusBadRequest, response.Code)
			require.NotContains(t, response.Body.String(), "X-HelpScout")
		})
	}
	fixture.requireNoEvent(t)
}

func TestConversationWebhookAnswers503WithoutASecretAndAcknowledgesOtherEvents(t *testing.T) {
	body := conversationJSON(501, "active", 123)
	unsigned := newWebhookFixture(t, helpscout.Config{}, "", helpscout.ConversationEventTriggerConfiguration{})
	require.Equal(t, http.StatusServiceUnavailable, unsigned.deliver(t, helpscout.WebhookEventConversationCreated, body, sentinelWebhookSecret),
		"Help Scout keeps retrying until a secret is configured")

	fixture := newWebhookFixture(t, helpscout.Config{}, sentinelWebhookSecret, helpscout.ConversationEventTriggerConfiguration{})
	require.Equal(t, http.StatusOK, fixture.deliver(t, "convo.deleted", `{"id":501}`, sentinelWebhookSecret))
	require.Equal(t, http.StatusOK, fixture.deliver(t, "satisfaction.ratings", `{"id":1}`, sentinelWebhookSecret))
	fixture.requireNoEvent(t)

	small := newWebhookFixture(t, helpscout.Config{WebhookMaxBodyBytes: 64}, sentinelWebhookSecret, helpscout.ConversationEventTriggerConfiguration{})
	require.Equal(t, http.StatusRequestEntityTooLarge, small.deliver(t, helpscout.WebhookEventConversationCreated, body, sentinelWebhookSecret))
	response := httptest.NewRecorder()
	small.handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/webhooks/helpscout", nil))
	require.Equal(t, http.StatusMethodNotAllowed, response.Code)
}

func TestConversationWebhookBindingFiltersByEventAndInbox(t *testing.T) {
	fixture := newWebhookFixture(t, helpscout.Config{}, sentinelWebhookSecret, helpscout.ConversationEventTriggerConfiguration{
		Events: []string{helpscout.WebhookEventConversationCreated, helpscout.WebhookEventCustomerReplyCreated}, MailboxID: 123,
	})
	require.Equal(t, http.StatusOK, fixture.deliver(t, helpscout.WebhookEventNoteCreated, conversationJSON(501, "active", 123), sentinelWebhookSecret),
		"a filtered event is still acknowledged")
	require.Equal(t, http.StatusOK, fixture.deliver(t, helpscout.WebhookEventConversationCreated, conversationJSON(502, "active", 999), sentinelWebhookSecret))
	require.Equal(t, http.StatusOK, fixture.deliver(t, helpscout.WebhookEventCustomerReplyCreated, conversationJSON(503, "active", 123), sentinelWebhookSecret))
	require.EqualValues(t, 503, fixture.receiveEvent(t).Payload.Conversation.ID)
	fixture.requireNoEvent(t)
}

func TestConversationEventTriggerConfigurationValidates(t *testing.T) {
	for _, configuration := range []helpscout.ConversationEventTriggerConfiguration{
		{Events: []string{"customer.created"}},
		{Events: []string{"convo.created", "convo.created"}},
		{MailboxID: -1},
	} {
		require.Error(t, configuration.Validate(), configuration)
	}
	require.NoError(t, helpscout.ConversationEventTriggerConfiguration{}.Validate())
	require.NoError(t, helpscout.ConversationEventTriggerConfiguration{Events: helpscout.ConversationWebhookEvents(), MailboxID: 123}.Validate())
	require.Len(t, helpscout.ConversationWebhookEvents(), 10)
}
