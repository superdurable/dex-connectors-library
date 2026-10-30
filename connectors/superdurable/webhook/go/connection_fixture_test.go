// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webhook_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// sentinelSecret stands in for every signing secret; no result, receipt, or response may contain it.
	sentinelSecret = "SENTINEL-SIGNING-SECRET"
	// standardSecret is a whsec_ secret whose key is the bytes of "standard-webhooks-test-key".
	standardSecret = "whsec_c3RhbmRhcmQtd2ViaG9va3MtdGVzdC1rZXk="
)

var (
	testConnection = sdkgo.ConnectionRef{Provider: "webhook", Name: "forms"}
	fixedNow       = time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)
)

// webhookFixture is one connection whose requestReceived Trigger runs until the test ends.
type webhookFixture struct {
	handler http.Handler
	events  chan sdkgo.TriggerEvent[webhook.WebhookRequestEvent]
}

func newWebhookFixture(t *testing.T, config webhook.Config, secret string, configuration webhook.RequestReceivedTriggerConfiguration) *webhookFixture {
	t.Helper()
	connection := newTestConnection(t, config, secret)
	handler, err := connection.RequestReceivedWebhookHandler()
	require.NoError(t, err)
	fixture := &webhookFixture{handler: handler, events: make(chan sdkgo.TriggerEvent[webhook.WebhookRequestEvent], 16)}
	runner := webhook.NewRequestReceivedTrigger(webhook.RequestReceivedTriggerConfig{
		Connection: connection, ConnectionName: testConnection.Name, BindingName: "submissions",
		Configuration: configuration,
		Target: sdkgo.TriggerTargetFunc[webhook.WebhookRequestEvent](func(_ context.Context, event sdkgo.TriggerEvent[webhook.WebhookRequestEvent]) error {
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

func newTestConnection(t *testing.T, config webhook.Config, secret string, options ...webhook.Option) webhook.Connection {
	t.Helper()
	client, err := webhook.New(config, staticCredentials(secret),
		append([]webhook.Option{webhook.WithClock(func() time.Time { return fixedNow })}, options...)...)
	require.NoError(t, err)
	connection, err := webhook.NewConnection(client, testConnection)
	require.NoError(t, err)
	return connection
}

func staticCredentials(secret string) sdkgo.StaticCredentialProvider[webhook.Credentials] {
	return sdkgo.StaticCredentialProvider[webhook.Credentials]{testConnection: {SigningSecret: sdkgo.NewSecretString(secret)}}
}

// post serves one request and returns its status; the body never echoes verifier text.
func (fixture *webhookFixture) post(t *testing.T, contentType string, body string, headers map[string]string) int {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/webhooks/forms", strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	require.NotContains(t, response.Body.String(), sentinelSecret)
	return response.Code
}

func (fixture *webhookFixture) receiveEvent(t *testing.T) sdkgo.TriggerEvent[webhook.WebhookRequestEvent] {
	t.Helper()
	select {
	case event := <-fixture.events:
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("the verified event was not delivered")
		return sdkgo.TriggerEvent[webhook.WebhookRequestEvent]{}
	}
}

func hmacSHA256(key []byte, contents string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(contents))
	return mac.Sum(nil)
}

func hexSignature(secret string, body string) string {
	return hex.EncodeToString(hmacSHA256([]byte(secret), body))
}

func base64Signature(secret string, body string) string {
	return base64.StdEncoding.EncodeToString(hmacSHA256([]byte(secret), body))
}

// standardWebhooksHeaders signs body as https://www.standardwebhooks.com specifies.
func standardWebhooksHeaders(key []byte, webhookID string, sentAt time.Time, body string) map[string]string {
	timestamp := strconv.FormatInt(sentAt.Unix(), 10)
	signature := base64.StdEncoding.EncodeToString(hmacSHA256(key, webhookID+"."+timestamp+"."+body))
	return map[string]string{"webhook-id": webhookID, "webhook-timestamp": timestamp, "webhook-signature": "v1," + signature}
}

func standardKey(t *testing.T) []byte {
	t.Helper()
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(standardSecret, "whsec_"))
	require.NoError(t, err)
	return key
}

// stepContext is the Dex Step context a Connector Step receives; one step value is one Step execution.
type stepContext struct {
	context.Context
	step string
}

func newStepContext(step string) *stepContext {
	return &stepContext{Context: context.Background(), step: step}
}

func (*stepContext) FlowID() string                                  { return "webhook-form-submission-test" }
func (*stepContext) RunID() string                                   { return "run" }
func (*stepContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (context *stepContext) StepExecutionID() string                 { return context.step }
func (*stepContext) FromStepExecutionID() string                     { return "" }
func (*stepContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*stepContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (*stepContext) Attempt() int32                                  { return 1 }
func (*stepContext) HasTimerFired() bool                             { return false }
func (*stepContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*stepContext) WaitForMethodFailed() bool                       { return false }
func (*stepContext) RecordHeartbeat(any) error                       { return nil }
func (*stepContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*stepContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*stepContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*stepContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*stepContext)(nil)
