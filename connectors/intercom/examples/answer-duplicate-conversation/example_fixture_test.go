// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
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
	"github.com/superdurable/dex-connectors-library/connectors/intercom"
	answerduplicate "github.com/superdurable/dex-connectors-library/connectors/intercom/examples/answer-duplicate-conversation/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

// exampleSetup is one connection file, its use-configuration sidecar, and the addresses run reads from the environment.
type exampleSetup struct {
	directory      string
	configPath     string
	webhookAddress string
	logs           *recordedLogs
}

// newExampleSetup writes the connection Dex Web saves, the adminPicker's choice, and the topic binding.
func newExampleSetup(t *testing.T, provider *fakeIntercom, dexAddress string) *exampleSetup {
	t.Helper()
	directory := t.TempDir()
	connections, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{map[string]any{
			"connectorId": intercom.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/intercom",
			"moduleVersion": "v0.1.0", "provider": "intercom", "connectionName": answerduplicate.ConnectionName,
			"configuration": map[string]any{"region": "us"},
			"credentials":   map[string]any{"access_token": fakeAccessToken, "client_secret": fakeClientSecret},
		}},
		"triggerBindings": []any{map[string]any{
			"connectorId": intercom.ConnectorID, "connectionName": answerduplicate.ConnectionName,
			"triggerName": intercom.ConversationEventTriggerDefinition.Trigger.TriggerName, "bindingName": answerduplicate.InboundTriggerBinding,
			"configuration": map[string]any{"topics": []string{intercom.TopicConversationUserCreated}},
		}},
	})
	require.NoError(t, err)
	reference := answerduplicate.ReplyConfigurationRef()
	useConfigurations, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.UseConfigurationsSchemaVersion,
		"operationConfigurations": []any{map[string]any{
			"connectorId": reference.ConnectorID, "connectionName": reference.ConnectionName, "operationId": reference.OperationID,
			"flowType": reference.FlowType, "stepType": reference.StepType, "configuration": map[string]any{"adminId": fakeAdminID},
		}},
	})
	require.NoError(t, err)
	setup := &exampleSetup{
		directory: directory, configPath: filepath.Join(directory, "connections.json"),
		webhookAddress: "127.0.0.1:" + unusedPort(t), logs: newRecordedLogs(),
	}
	require.NoError(t, os.WriteFile(setup.configPath, connections, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(directory, localconfig.UseConfigurationsFileName), useConfigurations, 0o600))
	t.Setenv(localconfig.EnvironmentVariable, setup.configPath)
	t.Setenv("DEX_FLOW_SERVICE_ADDRESS", dexAddress)
	t.Setenv("INTERCOM_WEBHOOK_BIND_ADDRESS", setup.webhookAddress)
	t.Setenv(localAPIBaseURLEnvironmentVariable, provider.URL)
	t.Cleanup(func() {
		if t.Failed() || testing.Verbose() {
			t.Logf("captured logs:\n%s", setup.logs.text())
		}
	})
	return setup
}

// newEmptyUseConfigurationStore loads a copy of the connection file without the use-configuration sidecar.
func newEmptyUseConfigurationStore(t *testing.T, configPath string) *localconfig.Store {
	t.Helper()
	contents, err := os.ReadFile(configPath)
	require.NoError(t, err)
	copyPath := filepath.Join(t.TempDir(), "connections.json")
	require.NoError(t, os.WriteFile(copyPath, contents, 0o600))
	store, err := localconfig.LoadFile(copyPath)
	require.NoError(t, err)
	return store
}

// runningExample is one run of the example's run function.
type runningExample struct {
	cancel context.CancelFunc
	result chan error
}

// startExample calls run with a fresh Worker port and blob cache, then waits for the readiness check.
func (setup *exampleSetup) startExample(t *testing.T) *runningExample {
	t.Helper()
	t.Setenv("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:"+unusedPort(t))
	t.Setenv("DEX_BLOB_CACHE_DIR", filepath.Join(setup.directory, "blobs-"+strconv.FormatInt(time.Now().UnixNano(), 10)))
	ctx, cancel := context.WithCancel(context.Background())
	running := &runningExample{cancel: cancel, result: make(chan error, 1)}
	go func() { running.result <- run(ctx, setup.logs.logger()) }()
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

// postNotification sends one webhook request with the given signature and returns the status code.
func (setup *exampleSetup) postNotification(t *testing.T, method string, body []byte, signature string) int {
	t.Helper()
	request, err := http.NewRequest(method, "http://"+setup.webhookAddress+webhookPath, strings.NewReader(string(body)))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "intercom-parrot-service-client/1.0")
	if signature != "" {
		request.Header.Set("X-Hub-Signature", signature)
	}
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	responseBody, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.NotContains(t, string(responseBody), fakeClientSecret)
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
