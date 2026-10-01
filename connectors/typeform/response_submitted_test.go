// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package typeform_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/typeform"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// webhookFixture is one connection whose responseSubmitted binding runs until the test ends.
type webhookFixture struct {
	handler http.Handler
	events  chan sdkgo.TriggerEvent[typeform.FormResponseEvent]
}

func newWebhookFixture(
	t *testing.T, config typeform.Config, secret string, configuration typeform.ResponseSubmittedTriggerConfiguration,
) *webhookFixture {
	t.Helper()
	connection, fixture := newIdleWebhookFixture(t, config, secret)
	runner := typeform.NewResponseSubmittedTrigger(typeform.ResponseSubmittedTriggerConfig{
		Connection: connection, ConnectionName: testConnection.Name, BindingName: "submissions", Configuration: configuration,
		Target: sdkgo.TriggerTargetFunc[typeform.FormResponseEvent](func(_ context.Context, event sdkgo.TriggerEvent[typeform.FormResponseEvent]) error {
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
	readiness := fixture.handler.(interface{ RunningSourceCount() int })
	require.Eventually(t, func() bool { return readiness.RunningSourceCount() == 1 }, 5*time.Second, time.Millisecond)
	return fixture
}

// newIdleWebhookFixture mounts a connection's endpoint without running any binding.
func newIdleWebhookFixture(t *testing.T, config typeform.Config, secret string) (typeform.Connection, *webhookFixture) {
	t.Helper()
	connection, err := typeform.NewConnection(newTestClient(t, nil, personalAccessTokenCredentials(secret), config), testConnection)
	require.NoError(t, err)
	handler, err := connection.ResponseSubmittedWebhookHandler()
	require.NoError(t, err)
	return connection, &webhookFixture{handler: handler, events: make(chan sdkgo.TriggerEvent[typeform.FormResponseEvent], 16)}
}

// deliver posts body with header as its Typeform-Signature; a blank header sends none.
func (fixture *webhookFixture) deliver(t *testing.T, method string, body string, header string) int {
	t.Helper()
	request := httptest.NewRequest(method, "/webhooks/typeform", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if header != "" {
		request.Header.Set("Typeform-Signature", header)
	}
	recorder := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recorder, request)
	responseBody, err := io.ReadAll(recorder.Result().Body)
	require.NoError(t, err)
	require.NotContains(t, string(responseBody), sentinelSecret)
	return recorder.Code
}

func (fixture *webhookFixture) requireNoEvent(t *testing.T) {
	t.Helper()
	select {
	case event := <-fixture.events:
		t.Fatalf("unexpected event %s", event.ID)
	case <-time.After(50 * time.Millisecond):
	}
}

func (fixture *webhookFixture) nextEvent(t *testing.T) sdkgo.TriggerEvent[typeform.FormResponseEvent] {
	t.Helper()
	select {
	case event := <-fixture.events:
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("no event was delivered")
		return sdkgo.TriggerEvent[typeform.FormResponseEvent]{}
	}
}

// typeformSignature signs body the way Typeform's documentation does: sha256= and base64 HMAC-SHA256.
func typeformSignature(body string, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// webhookBody is a Typeform webhook whose form definition supplies refs the answers omit.
func webhookBody(t *testing.T, eventType string, formID string, token string) string {
	t.Helper()
	var formResponse map[string]any
	require.NoError(t, json.Unmarshal([]byte(formResponseJSON(token)), &formResponse))
	formResponse["form_id"] = formID
	formResponse["response_url"] = "https://admin.typeform.com/form/" + formID + "/results?responseId=" + token
	formResponse["definition"] = map[string]any{"id": formID, "title": "Webhooks example", "fields": []any{
		map[string]any{"id": "JwWggjAKtOkA", "title": "What is your first name?", "type": "short_text", "ref": "first_name"},
		map[string]any{"id": "KoJxDM3c6x8h", "title": "When did you move?", "type": "date", "ref": "moved_on"},
	}}
	encoded, err := json.Marshal(map[string]any{"event_id": "LtWXD3crgy", "event_type": eventType, "form_response": formResponse})
	require.NoError(t, err)
	return string(encoded)
}

func TestSignedSubmissionBecomesOneTypedEventKeyedByFormAndToken(t *testing.T) {
	fixture := newWebhookFixture(t, typeform.Config{}, sentinelSecret, typeform.ResponseSubmittedTriggerConfiguration{FormID: testFormID})
	body := webhookBody(t, typeform.WebhookEventTypeFormResponse, testFormID, "a3a12ec67a1365927098a606107fac15")

	require.Equal(t, http.StatusOK, fixture.deliver(t, http.MethodPost, body, typeformSignature(body, sentinelSecret)))
	event := fixture.nextEvent(t)
	require.Equal(t, "lT4Z3j:a3a12ec67a1365927098a606107fac15", event.ID)
	require.Equal(t, time.Date(2026, time.September, 30, 11, 58, 59, 0, time.UTC), event.OccurredAt)
	expected := expectedResponse("a3a12ec67a1365927098a606107fac15")
	expected.Answers[0].FieldTitle = "What is your first name?"
	expected.Answers[2].FieldRef, expected.Answers[2].FieldTitle = "moved_on", "When did you move?"
	require.Equal(t, typeform.FormResponseEvent{
		WebhookEventID: "LtWXD3crgy", FormID: testFormID, FormTitle: "Webhooks example", Response: expected,
	}, event.Payload)
	answer, isAnswered := event.Payload.Response.AnswerByFieldRef("moved_on")
	require.True(t, isAnswered, "a ref the answer omits comes from the form definition")
	require.Equal(t, "2005-10-15", answer.Date)
	requireSecretFree(t, event)

	// A redelivery, or a copy from a second webhook of the form, keeps the event ID.
	redelivery := strings.Replace(body, `"event_id":"LtWXD3crgy"`, `"event_id":"SecondHook1"`, 1)
	require.Equal(t, http.StatusOK, fixture.deliver(t, http.MethodPost, redelivery, typeformSignature(redelivery, sentinelSecret)))
	require.Equal(t, event.ID, fixture.nextEvent(t).ID)
}

func TestForgedOrUnsignedSubmissionsAreAnswered400(t *testing.T) {
	fixture := newWebhookFixture(t, typeform.Config{}, sentinelSecret, typeform.ResponseSubmittedTriggerConfiguration{})
	body := webhookBody(t, typeform.WebhookEventTypeFormResponse, testFormID, "token1")
	signature := typeformSignature(body, sentinelSecret)
	mac := hmac.New(sha256.New, []byte(sentinelSecret))
	mac.Write([]byte(body))
	for name, test := range map[string]struct{ body, header string }{
		"tampered body":       {strings.Replace(body, "ada@example.com", "eve@example.com", 1), signature},
		"another secret":      {body, typeformSignature(body, "another-secret")},
		"missing header":      {body, ""},
		"hex digest":          {body, "sha256=" + hex.EncodeToString(mac.Sum(nil))},
		"missing prefix":      {body, strings.TrimPrefix(signature, "sha256=")},
		"another algorithm":   {body, strings.Replace(signature, "sha256=", "sha1=", 1)},
		"truncated signature": {body, signature[:len(signature)-4]},
	} {
		require.Equal(t, http.StatusBadRequest, fixture.deliver(t, http.MethodPost, test.body, test.header), name)
	}
	fixture.requireNoEvent(t)
}

func TestVerifiedButUnusableDeliveriesAreAnswered400OrAcknowledged(t *testing.T) {
	fixture := newWebhookFixture(t, typeform.Config{}, sentinelSecret, typeform.ResponseSubmittedTriggerConfiguration{FormID: testFormID})
	partial := webhookBody(t, "form_response_partial", testFormID, "token1")
	require.Equal(t, http.StatusOK, fixture.deliver(t, http.MethodPost, partial, typeformSignature(partial, sentinelSecret)),
		"a partial response is acknowledged without a record")
	otherForm := webhookBody(t, typeform.WebhookEventTypeFormResponse, "otherForm1", "token2")
	require.Equal(t, http.StatusOK, fixture.deliver(t, http.MethodPost, otherForm, typeformSignature(otherForm, sentinelSecret)),
		"the binding filters another form but still acknowledges it")
	for name, body := range map[string]string{
		"not JSON":              `{"event_type":`,
		"no form_response":      `{"event_id":"x","event_type":"form_response"}`,
		"definition of a form":  strings.Replace(webhookBody(t, typeform.WebhookEventTypeFormResponse, testFormID, "token3"), `"id":"lT4Z3j"`, `"id":"otherForm1"`, 1),
		"no submission time":    strings.Replace(webhookBody(t, typeform.WebhookEventTypeFormResponse, testFormID, "token4"), `"submitted_at":"2026-09-30T11:58:59Z"`, `"submitted_at":""`, 1),
		"answer without value":  strings.Replace(webhookBody(t, typeform.WebhookEventTypeFormResponse, testFormID, "token5"), `"email":"ada@example.com",`, ``, 1),
		"token unsafe as an ID": webhookBody(t, typeform.WebhookEventTypeFormResponse, testFormID, "../token"),
	} {
		require.Equal(t, http.StatusBadRequest, fixture.deliver(t, http.MethodPost, body, typeformSignature(body, sentinelSecret)), name)
	}
	fixture.requireNoEvent(t)
}

func TestEndpointAsksTypeformToRetryWhileItCannotVerifyOrRecord(t *testing.T) {
	body := webhookBody(t, typeform.WebhookEventTypeFormResponse, testFormID, "token1")
	withoutSecret := newWebhookFixture(t, typeform.Config{}, "", typeform.ResponseSubmittedTriggerConfiguration{})
	require.Equal(t, http.StatusServiceUnavailable, withoutSecret.deliver(t, http.MethodPost, body, typeformSignature(body, sentinelSecret)),
		"a connection without webhook_secret answers 503 so Typeform retries")
	_, idle := newIdleWebhookFixture(t, typeform.Config{}, sentinelSecret)
	require.Equal(t, http.StatusServiceUnavailable, idle.deliver(t, http.MethodPost, body, typeformSignature(body, sentinelSecret)),
		"no binding runs yet")
}

func TestEndpointRejectsOtherMethodsAndOversizedBodies(t *testing.T) {
	fixture := newWebhookFixture(t, typeform.Config{WebhookMaxBodyBytes: 512}, sentinelSecret, typeform.ResponseSubmittedTriggerConfiguration{})
	body := webhookBody(t, typeform.WebhookEventTypeFormResponse, testFormID, "token1")
	require.Equal(t, http.StatusRequestEntityTooLarge, fixture.deliver(t, http.MethodPost, body, typeformSignature(body, sentinelSecret)))
	require.Equal(t, http.StatusMethodNotAllowed, fixture.deliver(t, http.MethodGet, "", ""))
	fixture.requireNoEvent(t)
}

func TestResponseSubmittedTriggerConfigurationAcceptsOnlyFormIDs(t *testing.T) {
	require.NoError(t, typeform.ResponseSubmittedTriggerConfiguration{}.Validate())
	require.NoError(t, typeform.ResponseSubmittedTriggerConfiguration{FormID: "u6nXL7"}.Validate())
	require.ErrorContains(t, typeform.ResponseSubmittedTriggerConfiguration{FormID: "https://form.typeform.com/to/u6nXL7"}.Validate(), "formId")
}
