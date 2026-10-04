// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook"
	formsubmission "github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook/examples/form-submission/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

// sentinelSecret stands in for the signing secret; no log record may contain it.
const sentinelSecret = "SENTINEL-FORM-SIGNING-SECRET"

// submissionBinding is the form-submission-received binding the README saves.
var submissionBinding = webhook.RequestReceivedTriggerConfiguration{MatchPointer: "/event_type", MatchValues: []string{"form_response"}}

// newExampleConnection builds the README's Typeform-style connection; a static credential replaces project storage.
func newExampleConnection(t *testing.T, deliveryURL string, options ...webhook.Option) webhook.Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "webhook", Name: formsubmission.ConnectionName}
	client, err := webhook.New(webhook.Config{
		SignatureHeader: "Typeform-Signature", SignaturePrefix: "sha256=", SignatureEncoding: webhook.SignatureEncodingBase64,
		EventIDPointer: "/event_id", ForwardedHeaders: []string{"User-Agent"}, DeliveryURL: deliveryURL,
	}, sdkgo.StaticCredentialProvider[webhook.Credentials]{reference: {SigningSecret: sdkgo.NewSecretString(sentinelSecret)}}, options...)
	require.NoError(t, err)
	connection, err := webhook.NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

// submissionEndpoint serves the example's target like newSubmissionEndpointRunner, without the durable project inbox.
type submissionEndpoint struct {
	server         *httptest.Server
	endpointRunner *webhooktrigger.EndpointRunner
	readiness      interface{ RunningSourceCount() int }
}

func newSubmissionEndpoint(
	t *testing.T, connection webhook.Connection, target sdkgo.TriggerTarget[webhook.WebhookRequestEvent],
) *submissionEndpoint {
	t.Helper()
	handler, err := connection.RequestReceivedWebhookHandler()
	require.NoError(t, err)
	trigger := webhook.NewRequestReceivedTrigger(webhook.RequestReceivedTriggerConfig{
		Connection: connection, ConnectionName: formsubmission.ConnectionName, BindingName: formsubmission.SubmissionTriggerBinding,
		Configuration: submissionBinding, Target: target,
	})
	endpointRunner, err := webhooktrigger.NewEndpointRunner(handler, trigger)
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.Handle(submissionPath, endpointRunner)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &submissionEndpoint{server: server, endpointRunner: endpointRunner, readiness: handler.(interface{ RunningSourceCount() int })}
}

// start runs the binding until the test ends and waits until it receives submissions.
func (endpoint *submissionEndpoint) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	runFinished := make(chan error, 1)
	go func() { runFinished <- endpoint.endpointRunner.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.ErrorIs(t, <-runFinished, context.Canceled)
	})
	require.Eventually(t, func() bool { return endpoint.readiness.RunningSourceCount() == 1 }, 10*time.Second, 10*time.Millisecond,
		"the webhook binding must start receiving")
}

// postSubmission sends a Typeform-style signed submission; tamper changes the body after signing.
func (endpoint *submissionEndpoint) postSubmission(t *testing.T, body string, isTampered bool) int {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(sentinelSecret))
	mac.Write([]byte(body))
	signature := "sha256=" + base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if isTampered {
		body = strings.Replace(body, "ada@", "eve@", 1)
	}
	request, err := http.NewRequest(http.MethodPost, endpoint.server.URL+submissionPath, strings.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Typeform-Signature", signature)
	request.Header.Set("User-Agent", "Typeform Webhooks")
	response, err := endpoint.server.Client().Do(request)
	require.NoError(t, err)
	responseBody, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.NotContains(t, string(responseBody), sentinelSecret)
	return response.StatusCode
}

func submissionBody(eventID string, eventType string) string {
	return fmt.Sprintf(`{"event_id":%q,"event_type":%q,"form_response":{"form_id":"contact","answers":[{"type":"email","email":"ada@example.com"}]}}`,
		eventID, eventType)
}

func unusedPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

// recordedLogs keeps every record, at every level, as a message plus flattened attributes.
type recordedLogs struct {
	mu      sync.Mutex
	records []recordedLog
	output  strings.Builder
}

type recordedLog struct {
	message string
	attrs   map[string]string
}

// newRecordedLogs keeps the test's records and prints them when the test fails or runs verbosely.
func newRecordedLogs(t *testing.T) *recordedLogs {
	t.Helper()
	logs := &recordedLogs{}
	t.Cleanup(func() {
		if t.Failed() || testing.Verbose() {
			t.Logf("captured logs:\n%s", logs.text())
		}
	})
	return logs
}

func (logs *recordedLogs) logger() *slog.Logger {
	return slog.New(recordedLogHandler{logs: logs})
}

// find returns the records with message whose attributes include every entry of attrs.
func (logs *recordedLogs) find(message string, attrs map[string]string) []recordedLog {
	logs.mu.Lock()
	defer logs.mu.Unlock()
	matches := []recordedLog{}
	for _, record := range logs.records {
		if record.message != message {
			continue
		}
		isMatch := true
		for key, value := range attrs {
			isMatch = isMatch && record.attrs[key] == value
		}
		if isMatch {
			matches = append(matches, record)
		}
	}
	return matches
}

func (logs *recordedLogs) text() string {
	logs.mu.Lock()
	defer logs.mu.Unlock()
	return logs.output.String()
}

type recordedLogHandler struct {
	logs  *recordedLogs
	attrs []slog.Attr
}

func (recordedLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (handler recordedLogHandler) Handle(_ context.Context, record slog.Record) error {
	attrs := map[string]string{}
	for _, attr := range handler.attrs {
		attrs[attr.Key] = attr.Value.String()
	}
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value.String()
		return true
	})
	handler.logs.mu.Lock()
	defer handler.logs.mu.Unlock()
	handler.logs.records = append(handler.logs.records, recordedLog{message: record.Message, attrs: attrs})
	fmt.Fprintf(&handler.logs.output, "level=%s msg=%q attrs=%v\n", record.Level, record.Message, attrs)
	return nil
}

func (handler recordedLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return recordedLogHandler{logs: handler.logs, attrs: append(append([]slog.Attr(nil), handler.attrs...), attrs...)}
}

func (handler recordedLogHandler) WithGroup(string) slog.Handler { return handler }
