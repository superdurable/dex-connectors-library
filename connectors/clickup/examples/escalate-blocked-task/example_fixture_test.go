// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
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
	"github.com/superdurable/dex-connectors-library/connectors/clickup"
	escalateblocked "github.com/superdurable/dex-connectors-library/connectors/clickup/examples/escalate-blocked-task/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

// blockedStatusBinding is the event binding Dex Web saves: only status changes reach the Flow's admission rule.
var blockedStatusBinding = clickup.TaskEventTriggerConfiguration{Events: []string{clickup.EventTaskStatusUpdated}}

// exampleSettings are the escalation settings the integration tests run with.
var exampleSettings = escalateblocked.EscalationSettings{
	WorkspaceID: fakeWorkspaceID, EscalationListID: fakeEscalationID, ManagerEmail: fakeManagerEmail, BlockedStatus: "blocked",
}

// newExampleConnection builds the connection Dex Web saves against the fake; a static credential replaces project storage.
func newExampleConnection(t *testing.T, provider *fakeClickUp, options ...clickup.Option) clickup.Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "clickup", Name: escalateblocked.ConnectionName}
	client, err := clickup.New(clickup.Config{}, sdkgo.StaticCredentialProvider[clickup.Credentials]{reference: {
		APIToken: sdkgo.NewSecretString(fakeAPIToken), WebhookSecret: sdkgo.NewSecretString(fakeWebhookSecret),
	}}, append([]clickup.Option{clickup.WithAPIBaseURL(provider.URL)}, options...)...)
	require.NoError(t, err)
	connection, err := clickup.NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

// blockedStatusEndpoint serves the example's target like newBlockedStatusEndpointRunner, without the durable project inbox.
type blockedStatusEndpoint struct {
	provider       *fakeClickUp
	server         *httptest.Server
	endpointRunner *webhooktrigger.EndpointRunner
}

func newBlockedStatusEndpoint(
	t *testing.T, provider *fakeClickUp, connection clickup.Connection, target sdkgo.TriggerTarget[clickup.TaskEvent],
) *blockedStatusEndpoint {
	t.Helper()
	handler, err := connection.TaskEventWebhookHandler()
	require.NoError(t, err)
	trigger := clickup.NewTaskEventTrigger(clickup.TaskEventTriggerConfig{
		Connection: connection, ConnectionName: escalateblocked.ConnectionName, BindingName: escalateblocked.BlockedStatusTriggerBinding,
		Configuration: blockedStatusBinding, Target: target,
	})
	endpointRunner, err := webhooktrigger.NewEndpointRunner(handler, trigger)
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.Handle(webhookPath, endpointRunner)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &blockedStatusEndpoint{provider: provider, server: server, endpointRunner: endpointRunner}
}

// start runs the binding until the test ends; an unadmitted status probe answers 503 until it receives.
func (endpoint *blockedStatusEndpoint) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	runFinished := make(chan error, 1)
	go func() { runFinished <- endpoint.endpointRunner.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.ErrorIs(t, <-runFinished, context.Canceled)
	})
	probe, probeSignature := signedStatusEvent("probe1", "probe"+strconv.FormatInt(time.Now().UnixNano(), 10), "in review", fakeWebhookSecret)
	require.Eventually(t, func() bool {
		status, _, err := endpoint.send(probe, probeSignature)
		return err == nil && status == http.StatusOK
	}, 10*time.Second, 10*time.Millisecond, "the webhook binding must start receiving")
}

// postEvent sends one webhook request with the given signature and returns the status code.
func (endpoint *blockedStatusEndpoint) postEvent(t *testing.T, body []byte, signature string) int {
	t.Helper()
	status, responseBody, err := endpoint.send(body, signature)
	require.NoError(t, err)
	require.NotContains(t, responseBody, fakeWebhookSecret)
	return status
}

// send sends one webhook request the way ClickUp does and returns the status and response body.
func (endpoint *blockedStatusEndpoint) send(body []byte, signature string) (int, string, error) {
	request, err := http.NewRequest(http.MethodPost, endpoint.server.URL+webhookPath, strings.NewReader(string(body)))
	if err != nil {
		return 0, "", err
	}
	request.Header.Set("Content-Type", "application/json")
	if signature != "" {
		request.Header.Set("X-Signature", signature)
	}
	response, err := endpoint.server.Client().Do(request)
	if err != nil {
		return 0, "", err
	}
	responseBody, err := io.ReadAll(response.Body)
	return response.StatusCode, string(responseBody), errors.Join(err, response.Body.Close())
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
