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
	githubconnector "github.com/superdurable/dex-connectors-library/connectors/github"
	repositorychanges "github.com/superdurable/dex-connectors-library/connectors/github/examples/repository-changes/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestNewLoggerUsesLogLevel(t *testing.T) {
	for _, testCase := range []struct {
		levelName      string
		isDebugEnabled bool
		hasWarning     bool
	}{
		{"", false, false}, {"info", false, false}, {"DEBUG", true, false}, {" warn ", false, false}, {"verbose", false, true},
	} {
		var output bytes.Buffer
		logger := newLogger(&output, testCase.levelName)
		require.Equal(t, testCase.isDebugEnabled, logger.Enabled(context.Background(), slog.LevelDebug), testCase.levelName)
		if testCase.hasWarning {
			require.Contains(t, output.String(), `level=WARN msg="LOG_LEVEL is not debug, info, warn, or error; using info" log_level=verbose`)
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
	}, newLogger(output, "info"))
	require.NoError(t, err)
	require.Equal(t, 3, attempts)
	logs := output.String()
	require.Contains(t, logs, `level=WARN msg="dex server unavailable; retrying" attempt=1 delay=250ms error="connection refused"`)
	require.Contains(t, logs, `level=WARN msg="dex server unavailable; retrying" attempt=2 delay=500ms error="connection refused"`)
	require.Contains(t, logs, `level=INFO msg="dex server available after retry" attempts=3`)
}

// TestRunWaitsForUnreachableDexServerAndStopsCleanly loads the Dex Web connection file and starts the
// example while no Dex Server is listening.
func TestRunWaitsForUnreachableDexServerAndStopsCleanly(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "connections.json")
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{map[string]any{
			"connectorId": githubconnector.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/github",
			"moduleVersion": "v0.7.0", "provider": "github", "connectionName": repositorychanges.ConnectionName,
			"configuration": map[string]any{"baseUrl": "http://127.0.0.1:1", "maxPatchCharacters": 2000},
			"credentials":   map[string]any{"access_token": "SENTINEL-ACCESS-TOKEN"},
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
	go func() { result <- run(ctx, newLogger(output, "info")) }()
	require.Eventually(t, func() bool {
		return strings.Contains(output.String(), `msg="dex server unavailable; retrying" attempt=1 delay=250ms`)
	}, 10*time.Second, 50*time.Millisecond, "the example must wait for the Dex Server instead of exiting")
	require.Empty(t, result)
	cancel()
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("the example did not stop after cancellation")
	}
	require.NotContains(t, output.String(), "SENTINEL")
}

func TestRunRejectsAMissingConnection(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "connections.json")
	contents, err := json.Marshal(map[string]any{"schemaVersion": localconfig.SchemaVersion, "connections": []any{}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, contents, 0o600))
	t.Setenv(localconfig.EnvironmentVariable, configPath)
	err = run(context.Background(), newLogger(&lockedBuffer{}, "info"))
	require.Error(t, err)
	require.Contains(t, err.Error(), repositorychanges.ConnectionName)
}

// lockedBuffer collects log output written by the example's goroutine while the test reads it.
type lockedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (buffer *lockedBuffer) Write(value []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.buffer.Write(value)
}

func (buffer *lockedBuffer) String() string {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.buffer.String()
}

// unusedPort returns a free local port for a Worker bind address that the test never starts.
func unusedPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}
