// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webhook_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook"
)

func signedHeaders(body string, extra map[string]string) map[string]string {
	headers := map[string]string{"X-Signature-256": hexSignature(sentinelSecret, body)}
	for name, value := range extra {
		headers[name] = value
	}
	return headers
}

func TestEventIDPrefersTheHeaderThenThePointerThenTheBodyDigest(t *testing.T) {
	bodyDigest := sha256.Sum256([]byte(submissionBody))
	for _, test := range []struct {
		name       string
		config     webhook.Config
		headers    map[string]string
		expectedID string
	}{
		{name: "configured header present", config: webhook.Config{EventIDHeader: "X-GitHub-Delivery", EventIDPointer: "/event_id"},
			headers: map[string]string{"X-Github-Delivery": "72d3162e-cc78-11e3-81ab-4c9367dc0958"}, expectedID: "72d3162e-cc78-11e3-81ab-4c9367dc0958"},
		{name: "configured header absent falls back to the pointer", config: webhook.Config{EventIDHeader: "X-GitHub-Delivery", EventIDPointer: "/event_id"},
			expectedID: "evt_1"},
		{name: "configured header absent without a pointer uses the digest", config: webhook.Config{EventIDHeader: "X-GitHub-Delivery"},
			expectedID: hex.EncodeToString(bodyDigest[:])},
		{name: "no configured source uses the digest", config: webhook.Config{}, expectedID: hex.EncodeToString(bodyDigest[:])},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWebhookFixture(t, test.config, sentinelSecret, webhook.RequestReceivedTriggerConfiguration{})
			require.Equal(t, http.StatusOK, fixture.post(t, webhook.ContentTypeJSON, submissionBody, signedHeaders(submissionBody, test.headers)))
			require.Equal(t, test.expectedID, fixture.receiveEvent(t).ID)
		})
	}
}

func TestStandardWebhooksAlwaysUsesTheWebhookID(t *testing.T) {
	fixture := newWebhookFixture(t, webhook.Config{
		VerificationScheme: webhook.VerificationSchemeStandardWebhooks, EventIDHeader: "X-Delivery", EventIDPointer: "/event_id",
	}, standardSecret, webhook.RequestReceivedTriggerConfiguration{})
	headers := standardWebhooksHeaders(standardKey(t), "msg_2KWPBgLlAfxdpx2AI54pPJ85f4W", fixedNow, submissionBody)
	headers["X-Delivery"] = "delivery-header"
	require.Equal(t, http.StatusOK, fixture.post(t, webhook.ContentTypeJSON, submissionBody, headers))
	require.Equal(t, "msg_2KWPBgLlAfxdpx2AI54pPJ85f4W", fixture.receiveEvent(t).ID)
}

func TestEventIDPointerReadsNumbersAndFormFieldsAndRejectsMissingIDs(t *testing.T) {
	numeric := `{"submission":{"id":42},"items":[{"id":"a"}]}`
	fixture := newWebhookFixture(t, webhook.Config{EventIDPointer: "/submission/id"}, sentinelSecret, webhook.RequestReceivedTriggerConfiguration{})
	require.Equal(t, http.StatusOK, fixture.post(t, webhook.ContentTypeJSON, numeric, signedHeaders(numeric, nil)))
	require.Equal(t, "42", fixture.receiveEvent(t).ID)

	formFixture := newWebhookFixture(t, webhook.Config{EventIDPointer: "/submission_id/0"}, sentinelSecret, webhook.RequestReceivedTriggerConfiguration{})
	form := "submission_id=sub-7&email=ada%40example.com"
	require.Equal(t, http.StatusOK, formFixture.post(t, webhook.ContentTypeForm, form, signedHeaders(form, nil)))
	require.Equal(t, "sub-7", formFixture.receiveEvent(t).ID)

	for name, body := range map[string]string{
		"missing value":      `{"submission":{}}`,
		"empty string":       `{"submission":{"id":""}}`,
		"object value":       `{"submission":{"id":{"nested":true}}}`,
		"null value":         `{"submission":{"id":null}}`,
		"value with a space": `{"submission":{"id":"two words"}}`,
		"oversized value":    `{"submission":{"id":"` + strings.Repeat("x", 257) + `"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, http.StatusBadRequest, fixture.post(t, webhook.ContentTypeJSON, body, signedHeaders(body, nil)))
		})
	}
	require.Empty(t, fixture.events)
}

func TestContentTypesKeepJSONAndDecodeFormsAndRejectOthers(t *testing.T) {
	fixture := newWebhookFixture(t, webhook.Config{}, sentinelSecret, webhook.RequestReceivedTriggerConfiguration{})
	require.Equal(t, http.StatusOK, fixture.post(t, "application/json; charset=utf-8", submissionBody, signedHeaders(submissionBody, nil)))
	jsonEvent := fixture.receiveEvent(t).Payload
	require.Equal(t, webhook.ContentTypeJSON, jsonEvent.ContentType)
	require.Equal(t, submissionBody, string(jsonEvent.JSONBody), "the body is kept byte for byte")
	require.Nil(t, jsonEvent.FormBody)
	require.Equal(t, fixedNow, jsonEvent.ReceivedAt)

	form := "name=Ada+Lovelace&topic=engines&topic=notes"
	require.Equal(t, http.StatusOK, fixture.post(t, webhook.ContentTypeForm, form, signedHeaders(form, nil)))
	formEvent := fixture.receiveEvent(t).Payload
	require.Equal(t, webhook.ContentTypeForm, formEvent.ContentType)
	require.Equal(t, map[string][]string{"name": {"Ada Lovelace"}, "topic": {"engines", "notes"}}, formEvent.FormBody)
	require.Nil(t, formEvent.JSONBody)

	for _, test := range []struct{ name, contentType, body string }{
		{name: "plain text", contentType: "text/plain", body: "hello"},
		{name: "missing content type", contentType: "", body: submissionBody},
		{name: "invalid JSON", contentType: webhook.ContentTypeJSON, body: `{"event_id":`},
		{name: "empty JSON body", contentType: webhook.ContentTypeJSON, body: ""},
		{name: "invalid form escape", contentType: webhook.ContentTypeForm, body: "name=%zz"},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, http.StatusBadRequest, fixture.post(t, test.contentType, test.body, signedHeaders(test.body, nil)))
		})
	}
	require.Empty(t, fixture.events)
}

func TestForwardedHeadersAreCopiedAndCredentialHeadersAreDenied(t *testing.T) {
	fixture := newWebhookFixture(t, webhook.Config{ForwardedHeaders: []string{"x-github-event", "User-Agent", "X-Absent", "X-GitHub-Event"}},
		sentinelSecret, webhook.RequestReceivedTriggerConfiguration{})
	headers := signedHeaders(submissionBody, map[string]string{"X-GitHub-Event": "push", "User-Agent": "GitHub-Hookshot/1", "Authorization": "Bearer " + sentinelSecret})
	require.Equal(t, http.StatusOK, fixture.post(t, webhook.ContentTypeJSON, submissionBody, headers))
	require.Equal(t, map[string]string{"X-Github-Event": "push", "User-Agent": "GitHub-Hookshot/1"}, fixture.receiveEvent(t).Payload.Headers)

	for name, config := range map[string]webhook.Config{
		"authorization":            {ForwardedHeaders: []string{"authorization"}},
		"proxy authorization":      {ForwardedHeaders: []string{"Proxy-Authorization"}},
		"cookie":                   {ForwardedHeaders: []string{"Cookie"}},
		"default signature header": {ForwardedHeaders: []string{"x-signature-256"}},
		"custom signature header":  {SignatureHeader: "Typeform-Signature", ForwardedHeaders: []string{"typeform-signature"}},
		"token header":             {ForwardedHeaders: []string{"X-Webhook-Token"}},
		"standard signature":       {ForwardedHeaders: []string{"Webhook-Signature"}},
		"invalid name":             {ForwardedHeaders: []string{"X Event"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := webhook.New(config, staticCredentials(sentinelSecret))
			require.Error(t, err)
		})
	}
}

func TestBindingMatchAcceptsOnlyConfiguredValuesAndAcknowledgesTheRest(t *testing.T) {
	fixture := newWebhookFixture(t, webhook.Config{EventIDPointer: "/event_id"}, sentinelSecret, webhook.RequestReceivedTriggerConfiguration{
		MatchPointer: "/event_type", MatchValues: []string{"form_response", "42", "true"},
	})
	for _, body := range []string{
		`{"event_id":"evt_partial","event_type":"form_response_partial"}`,
		`{"event_id":"evt_missing"}`,
		`{"event_id":"evt_object","event_type":{"name":"form_response"}}`,
		`{"event_id":"evt_complete","event_type":"form_response"}`,
		`{"event_id":"evt_number","event_type":42}`,
		`{"event_id":"evt_boolean","event_type":true}`,
	} {
		require.Equal(t, http.StatusOK, fixture.post(t, webhook.ContentTypeJSON, body, signedHeaders(body, nil)), "a filtered event is still acknowledged")
	}
	for _, expectedID := range []string{"evt_complete", "evt_number", "evt_boolean"} {
		require.Equal(t, expectedID, fixture.receiveEvent(t).ID, "events arrive in order, so skipped events were never delivered")
	}

	formFixture := newWebhookFixture(t, webhook.Config{}, sentinelSecret, webhook.RequestReceivedTriggerConfiguration{
		MatchPointer: "/form/0", MatchValues: []string{"contact"},
	})
	for _, form := range []string{"form=newsletter&id=1", "form=contact&id=2"} {
		require.Equal(t, http.StatusOK, formFixture.post(t, webhook.ContentTypeForm, form, signedHeaders(form, nil)))
	}
	require.Equal(t, []string{"contact"}, formFixture.receiveEvent(t).Payload.FormBody["form"])
}

func TestBindingConfigurationAndPointersAreValidated(t *testing.T) {
	for name, configuration := range map[string]webhook.RequestReceivedTriggerConfiguration{
		"pointer without values": {MatchPointer: "/event_type"},
		"values without pointer": {MatchValues: []string{"form_response"}},
		"pointer without slash":  {MatchPointer: "event_type", MatchValues: []string{"x"}},
		"invalid escape":         {MatchPointer: "/a~2b", MatchValues: []string{"x"}},
		"dangling escape":        {MatchPointer: "/a~", MatchValues: []string{"x"}},
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, configuration.Validate())
		})
	}
	require.NoError(t, webhook.RequestReceivedTriggerConfiguration{}.Validate())
	_, err := webhook.New(webhook.Config{EventIDPointer: "id"}, staticCredentials(sentinelSecret))
	require.Error(t, err)
}

func TestPointersUnescapeKeysAndIndexArraysStrictly(t *testing.T) {
	body := `{"a/b":{"m~n":["zero","one"]},"list":["first"]}`
	for pointer, expectedID := range map[string]string{"/a~1b/m~0n/1": "one", "/list/0": "first"} {
		t.Run(pointer, func(t *testing.T) {
			fixture := newWebhookFixture(t, webhook.Config{EventIDPointer: pointer}, sentinelSecret, webhook.RequestReceivedTriggerConfiguration{})
			require.Equal(t, http.StatusOK, fixture.post(t, webhook.ContentTypeJSON, body, signedHeaders(body, nil)))
			require.Equal(t, expectedID, fixture.receiveEvent(t).ID)
		})
	}
	for _, pointer := range []string{"/list/01", "/list/-", "/list/1", "/list/x"} {
		t.Run(pointer, func(t *testing.T) {
			fixture := newWebhookFixture(t, webhook.Config{EventIDPointer: pointer}, sentinelSecret, webhook.RequestReceivedTriggerConfiguration{})
			require.Equal(t, http.StatusBadRequest, fixture.post(t, webhook.ContentTypeJSON, body, signedHeaders(body, nil)))
		})
	}
}
