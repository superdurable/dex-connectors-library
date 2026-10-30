// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webhook_test

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook"
)

const submissionBody = `{"event_id":"evt_1","event_type":"form_response","email":"ada@example.com"}`

func TestHMACSHA256VerificationAcceptsOnlyTheConfiguredSignatureShape(t *testing.T) {
	for _, test := range []struct {
		name    string
		config  webhook.Config
		headers map[string]string
	}{
		{name: "default header with hex", config: webhook.Config{},
			headers: map[string]string{"X-Signature-256": hexSignature(sentinelSecret, submissionBody)}},
		{name: "GitHub prefix with hex", config: webhook.Config{SignatureHeader: "X-Hub-Signature-256", SignaturePrefix: "sha256="},
			headers: map[string]string{"X-Hub-Signature-256": "sha256=" + hexSignature(sentinelSecret, submissionBody)}},
		{name: "uppercase hex", config: webhook.Config{},
			headers: map[string]string{"X-Signature-256": strings.ToUpper(hexSignature(sentinelSecret, submissionBody))}},
		{name: "Shopify base64 without prefix", config: webhook.Config{SignatureHeader: "x-shopify-hmac-sha256", SignatureEncoding: webhook.SignatureEncodingBase64},
			headers: map[string]string{"X-Shopify-Hmac-Sha256": base64Signature(sentinelSecret, submissionBody)}},
		{name: "Typeform prefix with base64", config: webhook.Config{SignatureHeader: "Typeform-Signature", SignaturePrefix: "sha256=", SignatureEncoding: webhook.SignatureEncodingBase64},
			headers: map[string]string{"Typeform-Signature": "sha256=" + base64Signature(sentinelSecret, submissionBody)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWebhookFixture(t, test.config, sentinelSecret, webhook.RequestReceivedTriggerConfiguration{})
			require.Equal(t, http.StatusOK, fixture.post(t, webhook.ContentTypeJSON, submissionBody, test.headers))
			require.JSONEq(t, submissionBody, string(fixture.receiveEvent(t).Payload.JSONBody))
		})
	}
}

func TestHMACSHA256VerificationRejectsTamperedOrMisencodedRequests(t *testing.T) {
	typeform := webhook.Config{SignatureHeader: "Typeform-Signature", SignaturePrefix: "sha256=", SignatureEncoding: webhook.SignatureEncodingBase64}
	validSignature := "sha256=" + base64Signature(sentinelSecret, submissionBody)
	for _, test := range []struct {
		name    string
		body    string
		headers map[string]string
	}{
		{name: "tampered body", body: strings.Replace(submissionBody, "ada", "eve", 1), headers: map[string]string{"Typeform-Signature": validSignature}},
		{name: "wrong secret", body: submissionBody, headers: map[string]string{"Typeform-Signature": "sha256=" + base64Signature("another-secret", submissionBody)}},
		{name: "missing header", body: submissionBody, headers: map[string]string{"X-Signature-256": validSignature}},
		{name: "missing prefix", body: submissionBody, headers: map[string]string{"Typeform-Signature": base64Signature(sentinelSecret, submissionBody)}},
		{name: "hex where base64 is configured", body: submissionBody, headers: map[string]string{"Typeform-Signature": "sha256=" + hexSignature(sentinelSecret, submissionBody)}},
		{name: "truncated signature", body: submissionBody, headers: map[string]string{"Typeform-Signature": validSignature[:20]}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWebhookFixture(t, typeform, sentinelSecret, webhook.RequestReceivedTriggerConfiguration{})
			require.Equal(t, http.StatusBadRequest, fixture.post(t, webhook.ContentTypeJSON, test.body, test.headers))
			require.Empty(t, fixture.events)
		})
	}
}

func TestStandardWebhooksVerificationChecksEverySignatureAndTheTimestampTolerance(t *testing.T) {
	config := webhook.Config{VerificationScheme: webhook.VerificationSchemeStandardWebhooks, TimestampToleranceSeconds: 60}
	key := standardKey(t)
	valid := standardWebhooksHeaders(key, "msg_valid", fixedNow, submissionBody)
	rotated := standardWebhooksHeaders(key, "msg_rotated", fixedNow, submissionBody)
	rotated["webhook-signature"] = "v1," + base64.StdEncoding.EncodeToString([]byte("an-old-signature-from-a-rotated-key")) +
		" v1a,ZmFrZS1hc3ltbWV0cmljLXNpZ25hdHVyZQ== " + rotated["webhook-signature"]
	for _, test := range []struct {
		name         string
		headers      map[string]string
		expectedCode int
	}{
		{name: "one valid signature", headers: valid, expectedCode: http.StatusOK},
		{name: "valid signature among rotated and other versions", headers: rotated, expectedCode: http.StatusOK},
		{name: "at the tolerance edge", headers: standardWebhooksHeaders(key, "msg_edge", fixedNow.Add(-60*time.Second), submissionBody), expectedCode: http.StatusOK},
		{name: "older than the tolerance", headers: standardWebhooksHeaders(key, "msg_old", fixedNow.Add(-61*time.Second), submissionBody), expectedCode: http.StatusBadRequest},
		{name: "future-dated beyond the tolerance", headers: standardWebhooksHeaders(key, "msg_future", fixedNow.Add(61*time.Second), submissionBody), expectedCode: http.StatusBadRequest},
		{name: "wrong key", headers: standardWebhooksHeaders([]byte("another-key"), "msg_wrong", fixedNow, submissionBody), expectedCode: http.StatusBadRequest},
		{name: "signature of another webhook-id", headers: withHeader(valid, "webhook-id", "msg_replayed"), expectedCode: http.StatusBadRequest},
		{name: "missing signature header", headers: withoutHeader(valid, "webhook-signature"), expectedCode: http.StatusBadRequest},
		{name: "missing timestamp header", headers: withoutHeader(valid, "webhook-timestamp"), expectedCode: http.StatusBadRequest},
		{name: "non-integer timestamp", headers: withHeader(valid, "webhook-timestamp", "soon"), expectedCode: http.StatusBadRequest},
		{name: "unversioned signature", headers: withHeader(valid, "webhook-signature", strings.TrimPrefix(valid["webhook-signature"], "v1,")), expectedCode: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWebhookFixture(t, config, standardSecret, webhook.RequestReceivedTriggerConfiguration{})
			require.Equal(t, test.expectedCode, fixture.post(t, webhook.ContentTypeJSON, submissionBody, test.headers))
			if test.expectedCode == http.StatusOK {
				require.Equal(t, test.headers["webhook-id"], fixture.receiveEvent(t).ID)
			}
		})
	}
}

func TestStandardWebhooksTamperedBodyIsRejected(t *testing.T) {
	fixture := newWebhookFixture(t, webhook.Config{VerificationScheme: webhook.VerificationSchemeStandardWebhooks}, standardSecret,
		webhook.RequestReceivedTriggerConfiguration{})
	headers := standardWebhooksHeaders(standardKey(t), "msg_tampered", fixedNow, submissionBody)
	require.Equal(t, http.StatusBadRequest, fixture.post(t, webhook.ContentTypeJSON, strings.Replace(submissionBody, "ada", "eve", 1), headers))
}

func TestStandardWebhooksUsesTheBytesOfASecretWithoutThePrefix(t *testing.T) {
	fixture := newWebhookFixture(t, webhook.Config{VerificationScheme: webhook.VerificationSchemeStandardWebhooks}, sentinelSecret,
		webhook.RequestReceivedTriggerConfiguration{})
	headers := standardWebhooksHeaders([]byte(sentinelSecret), "msg_raw", fixedNow, submissionBody)
	require.Equal(t, http.StatusOK, fixture.post(t, webhook.ContentTypeJSON, submissionBody, headers))
	require.Equal(t, "msg_raw", fixture.receiveEvent(t).ID)
}

func TestSharedTokenVerificationComparesTheConfiguredHeader(t *testing.T) {
	config := webhook.Config{VerificationScheme: webhook.VerificationSchemeSharedToken, TokenHeader: "x-form-token"}
	for _, test := range []struct {
		name         string
		headers      map[string]string
		expectedCode int
	}{
		{name: "matching token", headers: map[string]string{"X-Form-Token": sentinelSecret}, expectedCode: http.StatusOK},
		{name: "wrong token", headers: map[string]string{"X-Form-Token": "guess"}, expectedCode: http.StatusBadRequest},
		{name: "token with a matching prefix", headers: map[string]string{"X-Form-Token": sentinelSecret + "-extra"}, expectedCode: http.StatusBadRequest},
		{name: "token in another header", headers: map[string]string{"X-Webhook-Token": sentinelSecret}, expectedCode: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWebhookFixture(t, config, sentinelSecret, webhook.RequestReceivedTriggerConfiguration{})
			require.Equal(t, test.expectedCode, fixture.post(t, webhook.ContentTypeJSON, submissionBody, test.headers))
		})
	}
}

func TestVerificationIsRetryableWhileTheSecretIsUnusable(t *testing.T) {
	for _, test := range []struct {
		name   string
		config webhook.Config
		secret string
	}{
		{name: "blank secret", config: webhook.Config{}, secret: ""},
		{name: "whsec_ secret that is not base64", config: webhook.Config{VerificationScheme: webhook.VerificationSchemeStandardWebhooks}, secret: "whsec_not base64!"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWebhookFixture(t, test.config, test.secret, webhook.RequestReceivedTriggerConfiguration{})
			headers := standardWebhooksHeaders([]byte("any"), "msg_unusable", fixedNow, submissionBody)
			headers["X-Signature-256"] = hexSignature(test.secret, submissionBody)
			require.Equal(t, http.StatusServiceUnavailable, fixture.post(t, webhook.ContentTypeJSON, submissionBody, headers))
		})
	}
}

func TestNewRejectsInvalidVerificationHeaders(t *testing.T) {
	for name, config := range map[string]webhook.Config{
		"signature header with a space": {SignatureHeader: "X Signature"},
		"token header with a colon":     {TokenHeader: "X-Token:"},
		"prefix with a newline":         {SignaturePrefix: "sha256=\n"},
		"unknown verification scheme":   {VerificationScheme: "none"},
		"unknown signature encoding":    {SignatureEncoding: "base32"},
		"negative tolerance":            {TimestampToleranceSeconds: -1},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := webhook.New(config, staticCredentials(sentinelSecret))
			require.Error(t, err)
		})
	}
}

func withHeader(headers map[string]string, name string, value string) map[string]string {
	copied := map[string]string{name: value}
	for existingName, existingValue := range headers {
		if existingName != name {
			copied[existingName] = existingValue
		}
	}
	return copied
}

func withoutHeader(headers map[string]string, name string) map[string]string {
	copied := map[string]string{}
	for existingName, existingValue := range headers {
		if existingName != name {
			copied[existingName] = existingValue
		}
	}
	return copied
}
