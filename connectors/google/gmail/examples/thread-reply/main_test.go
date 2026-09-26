// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gmail "github.com/superdurable/dex-connectors-library/connectors/google/gmail"
	threadreply "github.com/superdurable/dex-connectors-library/connectors/google/gmail/examples/thread-reply/flow"
	"github.com/superdurable/dex-connectors-library/connectors/google/gmail/internal/testlog"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestNewLoggerUsesLogLevel(t *testing.T) {
	for _, testCase := range []struct {
		levelName    string
		debugEnabled bool
		warning      bool
	}{
		{"", false, false}, {"info", false, false}, {"debug", true, false}, {"DEBUG", true, false},
		{" warn ", false, false}, {"verbose", false, true},
	} {
		var output bytes.Buffer
		logger := newLogger(&output, testCase.levelName)
		require.Equal(t, testCase.debugEnabled, logger.Enabled(context.Background(), slog.LevelDebug), testCase.levelName)
		require.True(t, logger.Enabled(context.Background(), slog.LevelError), testCase.levelName)
		if testCase.warning {
			require.Contains(t, output.String(), `level=WARN msg="LOG_LEVEL is not debug, info, warn, or error; using info" log_level=verbose`)
		} else {
			require.Empty(t, output.String(), testCase.levelName)
		}
	}
}

func TestWaitForDexServerRetriesUntilTheServerAnswers(t *testing.T) {
	logs := testlog.NewLogRecorder()
	attempts := 0
	err := waitForDexServer(context.Background(), func(context.Context) (dex.HealthInfo, error) {
		attempts++
		if attempts < 3 {
			return dex.HealthInfo{}, errors.New("connection refused")
		}
		return dex.HealthInfo{Condition: "SERVING"}, nil
	}, logs.Logger())
	require.NoError(t, err)
	require.Equal(t, 3, attempts)
	retries := logs.Find("dex server unavailable; retrying", nil)
	require.Len(t, retries, 2)
	for index, delay := range []string{"250ms", "500ms"} {
		require.Equal(t, slog.LevelWarn, retries[index].Level)
		require.Equal(t, map[string]string{"attempt": strconv.Itoa(index + 1), "delay": delay, "error": "connection refused"}, retries[index].Attrs)
	}
	recovered := logs.Find("dex server available after retry", nil)
	require.Len(t, recovered, 1)
	require.Equal(t, slog.LevelInfo, recovered[0].Level)
	require.Equal(t, map[string]string{"attempts": "3"}, recovered[0].Attrs)
}

func TestWaitForDexServerStopsWhenContextEnds(t *testing.T) {
	logs := testlog.NewLogRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- waitForDexServer(ctx, func(context.Context) (dex.HealthInfo, error) {
			cancel()
			return dex.HealthInfo{}, errors.New("connection refused")
		}, logs.Logger())
	}()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("waitForDexServer did not stop after cancellation")
	}
	require.Empty(t, logs.Records(), "a check that failed because the context ended is not a retry")
}

// TestRunWaitsForUnreachableDexServerAndStopsCleanly starts the example while no Dex Server is listening.
func TestRunWaitsForUnreachableDexServerAndStopsCleanly(t *testing.T) {
	logs := testlog.NewLogRecorder()
	gmailAPI := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, `{"error":{"code":401}}`, http.StatusUnauthorized)
	}))
	t.Cleanup(gmailAPI.Close)
	directory := t.TempDir()
	configPath := filepath.Join(directory, "connections.json")
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{map[string]any{
			"connectorId": gmail.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/google/gmail",
			"moduleVersion": "v0.9.0", "provider": "google", "connectionName": threadreply.ConnectionName,
			"configuration": map[string]any{"endpoint": gmailAPI.URL},
			"credentials":   map[string]any{"access_token": "SENTINEL-ACCESS-TOKEN", "primary_email": "owner@example.com"},
		}},
		"triggerBindings": []any{
			map[string]any{"connectorId": gmail.ConnectorID, "connectionName": threadreply.ConnectionName,
				"triggerName": "messageReceived", "bindingName": threadreply.StartTriggerBinding, "configuration": map[string]any{}},
			map[string]any{"connectorId": gmail.ConnectorID, "connectionName": threadreply.ConnectionName,
				"triggerName": "replyReceived", "bindingName": threadreply.ReplyTriggerBinding, "configuration": map[string]any{}},
		},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, contents, 0o600))
	// The example requires the reply message that Dex Web saves beside the connection file.
	reference := threadreply.ReplyMessageConfigurationRef()
	useConfigurations, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.UseConfigurationsSchemaVersion,
		"operationConfigurations": []any{map[string]any{
			"connectorId": reference.ConnectorID, "connectionName": reference.ConnectionName, "operationId": reference.OperationID,
			"flowType": reference.FlowType, "stepType": reference.StepType, "configuration": map[string]any{"textBody": "Processing complete."},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, localconfig.UseConfigurationsFileName), useConfigurations, 0o600))
	t.Setenv(localconfig.EnvironmentVariable, configPath)
	// A Unix socket that nothing listens on makes the Dex Server unreachable without binding a TCP port.
	t.Setenv("DEX_FLOW_SERVICE_ADDRESS", "unix://"+filepath.Join(os.TempDir(), fmt.Sprintf("dex-missing-%d.sock", time.Now().UnixNano())))
	t.Setenv("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:"+unusedPort(t))
	t.Setenv("DEX_BLOB_CACHE_DIR", filepath.Join(directory, "blobs"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- run(ctx, logs.Logger()) }()
	require.Never(t, func() bool { return len(result) > 0 }, 2*time.Second, 50*time.Millisecond,
		"the example must wait for the Dex Server instead of exiting")
	waits := logs.Find("dex server unavailable; retrying", nil)
	require.NotEmpty(t, waits)
	require.Equal(t, slog.LevelWarn, waits[0].Level)
	require.Equal(t, "1", waits[0].Attrs["attempt"])
	require.Equal(t, "250ms", waits[0].Attrs["delay"])
	require.NotContains(t, logs.Text(), "SENTINEL")
	cancel()
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("the example did not stop after cancellation")
	}
}

// unusedPort returns a free local port for a Worker bind address that the test never starts.
func unusedPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}
