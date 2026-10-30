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
	"github.com/superdurable/dex-connectors-library/connectors/intercom"
	answerduplicate "github.com/superdurable/dex-connectors-library/connectors/intercom/examples/answer-duplicate-conversation/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

// inboundBinding is the topic binding Dex Web saves: only a new customer conversation starts a Flow.
var inboundBinding = intercom.ConversationEventTriggerConfiguration{Topics: []string{intercom.TopicConversationUserCreated}}

// newExampleConnection builds the connection Dex Web saves against the fake; a static credential replaces project storage.
func newExampleConnection(t *testing.T, provider *fakeIntercom, options ...intercom.Option) intercom.Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "intercom", Name: answerduplicate.ConnectionName}
	client, err := intercom.New(intercom.Config{Region: intercom.RegionUs}, sdkgo.StaticCredentialProvider[intercom.Credentials]{reference: {
		AccessToken: sdkgo.NewSecretString(fakeAccessToken), ClientSecret: sdkgo.NewSecretString(fakeClientSecret),
	}}, append([]intercom.Option{intercom.WithAPIBaseURL(provider.URL)}, options...)...)
	require.NoError(t, err)
	connection, err := intercom.NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

// inboundEndpoint serves the example's target like newInboundEndpointRunner, without the durable project inbox.
type inboundEndpoint struct {
	provider       *fakeIntercom
	server         *httptest.Server
	endpointRunner *webhooktrigger.EndpointRunner
}

func newInboundEndpoint(
	t *testing.T, provider *fakeIntercom, connection intercom.Connection, target sdkgo.TriggerTarget[intercom.ConversationEvent],
) *inboundEndpoint {
	t.Helper()
	handler, err := connection.ConversationEventWebhookHandler()
	require.NoError(t, err)
	trigger := intercom.NewConversationEventTrigger(intercom.ConversationEventTriggerConfig{
		Connection: connection, ConnectionName: answerduplicate.ConnectionName, BindingName: answerduplicate.InboundTriggerBinding,
		Configuration: inboundBinding, Target: target,
	})
	endpointRunner, err := webhooktrigger.NewEndpointRunner(handler, trigger)
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.Handle(webhookPath, endpointRunner)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &inboundEndpoint{provider: provider, server: server, endpointRunner: endpointRunner}
}

// start runs the binding until the test ends; a rating probe the binding filters answers 503 until it receives.
func (endpoint *inboundEndpoint) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	runFinished := make(chan error, 1)
	go func() { runFinished <- endpoint.endpointRunner.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.ErrorIs(t, <-runFinished, context.Canceled)
	})
	probeContactID := endpoint.provider.seedContact("probe@example.com", "user")
	probeConversationID := endpoint.provider.seedConversation(probeContactID, "open", time.Minute, "A readiness probe.")
	probe, probeSignature := endpoint.provider.signedNotification("notif_probe_"+strconv.FormatInt(time.Now().UnixNano(), 10),
		intercom.TopicConversationRatingAdded, probeConversationID, fakeClientSecret)
	require.Eventually(t, func() bool {
		status, _, err := endpoint.send(http.MethodPost, probe, probeSignature)
		return err == nil && status == http.StatusOK
	}, 10*time.Second, 10*time.Millisecond, "the webhook binding must start receiving")
}

// postNotification sends one webhook request with the given signature and returns the status code.
func (endpoint *inboundEndpoint) postNotification(t *testing.T, method string, body []byte, signature string) int {
	t.Helper()
	status, responseBody, err := endpoint.send(method, body, signature)
	require.NoError(t, err)
	require.NotContains(t, responseBody, fakeClientSecret)
	return status
}

// send sends one webhook request the way Intercom does and returns the status and response body.
func (endpoint *inboundEndpoint) send(method string, body []byte, signature string) (int, string, error) {
	request, err := http.NewRequest(method, endpoint.server.URL+webhookPath, strings.NewReader(string(body)))
	if err != nil {
		return 0, "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "intercom-parrot-service-client/1.0")
	if signature != "" {
		request.Header.Set("X-Hub-Signature", signature)
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
