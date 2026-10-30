// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/gmail/internal/testlog"
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
