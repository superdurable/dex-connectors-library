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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gemini "github.com/superdurable/dex-connectors-library/connectors/google/gemini"
	generatesummary "github.com/superdurable/dex-connectors-library/connectors/google/gemini/examples/generate-summary/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/sdk-go/dex"
)

// lockedBuffer collects text log output from concurrent goroutines.
type lockedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (buffer *lockedBuffer) Write(contents []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.buffer.Write(contents)
}

func (buffer *lockedBuffer) String() string {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.buffer.String()
}

func TestNewLoggerUsesLogLevel(t *testing.T) {
	for _, testCase := range []struct {
		levelName      string
		isDebugEnabled bool
		shouldWarn     bool
	}{
		{"", false, false}, {"info", false, false}, {"debug", true, false}, {" warn ", false, false}, {"verbose", false, true},
	} {
		var output bytes.Buffer
		logger := newLogger(&output, testCase.levelName)
		require.Equal(t, testCase.isDebugEnabled, logger.Enabled(context.Background(), slog.LevelDebug), testCase.levelName)
		if testCase.shouldWarn {
			require.Contains(t, output.String(), `msg="LOG_LEVEL is not debug, info, warn, or error; using info" log_level=verbose`)
		} else {
			require.Empty(t, output.String(), testCase.levelName)
		}
	}
}

func TestWaitForDexServerRetriesUntilTheServerAnswers(t *testing.T) {
	output := &lockedBuffer{}
	attempts := 0
	err := waitForDexServer(context.Background(), func(context.Context) (dex.HealthInfo, error) {
		attempts++
		if attempts < 3 {
			return dex.HealthInfo{}, errors.New("connection refused")
		}
		return dex.HealthInfo{Condition: "SERVING"}, nil
	}, slog.New(slog.NewTextHandler(output, nil)))
	require.NoError(t, err)
	require.Equal(t, 3, attempts)
	require.Equal(t, 2, strings.Count(output.String(), `msg="dex server unavailable; retrying"`))
	require.Contains(t, output.String(), "attempt=1 delay=250ms")
	require.Contains(t, output.String(), "attempt=2 delay=500ms")
	require.Contains(t, output.String(), `msg="dex server available after retry" attempts=3`)
}

// TestRunWaitsForUnreachableDexServerAndStopsCleanly starts the example while no Dex Server is listening.
func TestRunWaitsForUnreachableDexServerAndStopsCleanly(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "connections.json")
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{map[string]any{
			"connectorId": gemini.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/google/gemini",
			"moduleVersion": "v0.1.0", "provider": "google", "connectionName": generatesummary.ConnectionName,
			"configuration": map[string]any{},
			"credentials":   map[string]any{"api_key": "AIzaSENTINEL-example-key"},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, contents, 0o600))
	t.Setenv(localconfig.EnvironmentVariable, configPath)
	// A Unix socket that nothing listens on makes the Dex Server unreachable without binding a TCP port.
	t.Setenv("DEX_FLOW_SERVICE_ADDRESS", "unix://"+filepath.Join(os.TempDir(), fmt.Sprintf("dex-missing-%d.sock", time.Now().UnixNano())))
	t.Setenv("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:"+unusedPort(t))
	t.Setenv("DEX_BLOB_CACHE_DIR", filepath.Join(directory, "blobs"))

	output := &lockedBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- run(ctx, slog.New(slog.NewTextHandler(output, nil))) }()
	require.Never(t, func() bool { return len(result) > 0 }, 2*time.Second, 50*time.Millisecond,
		"the example must wait for the Dex Server instead of exiting")
	require.Contains(t, output.String(), `msg="dex server unavailable; retrying" attempt=1 delay=250ms`)
	require.NotContains(t, output.String(), "SENTINEL")
	cancel()
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("the example did not stop after cancellation")
	}
}

func TestRunRequiresTheLocalConnectionFile(t *testing.T) {
	t.Setenv(localconfig.EnvironmentVariable, filepath.Join(t.TempDir(), "missing.json"))
	require.Error(t, run(context.Background(), slog.New(slog.NewTextHandler(&lockedBuffer{}, nil))))
}

// unusedPort returns a free local port for a Worker bind address that the test never starts.
func unusedPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}
