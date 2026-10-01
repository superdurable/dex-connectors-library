// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook"
	formsubmission "github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook/examples/form-submission/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

// sentinelSecret stands in for the signing secret; no log record may contain it.
const sentinelSecret = "SENTINEL-FORM-SIGNING-SECRET"

// exampleSetup is one connection file plus the addresses that run reads from the environment.
type exampleSetup struct {
	directory      string
	configPath     string
	webhookAddress string
	logs           *recordedLogs
}

// newExampleSetup writes a Typeform-style connection: base64 HMAC with a prefix and an event_id pointer.
func newExampleSetup(t *testing.T, deliveryURL string, dexAddress string) *exampleSetup {
	t.Helper()
	directory := t.TempDir()
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{map[string]any{
			"connectorId": webhook.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook",
			"moduleVersion": "v0.1.0", "provider": "webhook", "connectionName": formsubmission.ConnectionName,
			"configuration": map[string]any{
				"signatureHeader": "Typeform-Signature", "signaturePrefix": "sha256=", "signatureEncoding": "base64",
				"eventIdPointer": "/event_id", "forwardedHeaders": []string{"User-Agent"}, "deliveryUrl": deliveryURL,
			},
			"credentials": map[string]any{"signing_secret": sentinelSecret},
		}},
		"triggerBindings": []any{map[string]any{
			"connectorId": webhook.ConnectorID, "connectionName": formsubmission.ConnectionName, "triggerName": "requestReceived",
			"bindingName":   formsubmission.SubmissionTriggerBinding,
			"configuration": map[string]any{"matchPointer": "/event_type", "matchValues": []string{"form_response"}},
		}},
	})
	require.NoError(t, err)
	setup := &exampleSetup{
		directory: directory, configPath: filepath.Join(directory, "connections.json"),
		webhookAddress: "127.0.0.1:" + unusedPort(t), logs: newRecordedLogs(),
	}
	require.NoError(t, os.WriteFile(setup.configPath, contents, 0o600))
	t.Setenv(localconfig.EnvironmentVariable, setup.configPath)
	t.Setenv("DEX_FLOW_SERVICE_ADDRESS", dexAddress)
	t.Setenv("WEBHOOK_BIND_ADDRESS", setup.webhookAddress)
	t.Cleanup(func() {
		if t.Failed() || testing.Verbose() {
			t.Logf("captured logs:\n%s", setup.logs.text())
		}
	})
	return setup
}

// runningExample is one run of the example's run function.
type runningExample struct {
	cancel context.CancelFunc
	result chan error
}

// startExample calls run with a fresh Worker port and blob cache, then waits for the readiness check.
func (setup *exampleSetup) startExample(t *testing.T, connectionOptions ...webhook.Option) *runningExample {
	t.Helper()
	t.Setenv("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:"+unusedPort(t))
	t.Setenv("DEX_BLOB_CACHE_DIR", filepath.Join(setup.directory, "blobs-"+strconv.FormatInt(time.Now().UnixNano(), 10)))
	ctx, cancel := context.WithCancel(context.Background())
	running := &runningExample{cancel: cancel, result: make(chan error, 1)}
	go func() { running.result <- run(ctx, setup.logs.logger(), connectionOptions...) }()
	t.Cleanup(cancel)
	require.Eventually(t, func() bool {
		response, err := http.Get("http://" + setup.webhookAddress + readinessPath)
		if err != nil {
			return false
		}
		_ = response.Body.Close() // Only the status matters.
		return response.StatusCode == http.StatusOK
	}, 20*time.Second, 25*time.Millisecond, "the webhook binding must start receiving")
	return running
}

func (running *runningExample) stop(t *testing.T) {
	t.Helper()
	running.cancel()
	select {
	case err := <-running.result:
		require.NoError(t, err)
	case <-time.After(20 * time.Second):
		t.Fatal("the example did not stop after cancellation")
	}
}

// postSubmission sends a Typeform-style signed submission; tamper changes the body after signing.
func (setup *exampleSetup) postSubmission(t *testing.T, body string, isTampered bool) int {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(sentinelSecret))
	mac.Write([]byte(body))
	signature := "sha256=" + base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if isTampered {
		body = strings.Replace(body, "ada@", "eve@", 1)
	}
	request, err := http.NewRequest(http.MethodPost, "http://"+setup.webhookAddress+submissionPath, strings.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Typeform-Signature", signature)
	request.Header.Set("User-Agent", "Typeform Webhooks")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	responseBody, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.NotContains(t, string(responseBody), sentinelSecret)
	return response.StatusCode
}

func (setup *exampleSetup) pendingEventIDs(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(setup.directory, ".trigger-inbox-*.json"))
	require.NoError(t, err)
	eventIDs := []string{}
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		require.NoError(t, err)
		var inbox struct {
			Events []struct {
				EventID string `json:"eventId"`
			} `json:"events"`
		}
		require.NoError(t, json.Unmarshal(contents, &inbox))
		for _, event := range inbox.Events {
			eventIDs = append(eventIDs, event.EventID)
		}
	}
	return eventIDs
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

func newRecordedLogs() *recordedLogs { return &recordedLogs{} }

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
