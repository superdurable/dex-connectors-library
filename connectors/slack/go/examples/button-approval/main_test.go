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
	"github.com/superdurable/dex-connectors-library/connectors/slack/go/internal/testlog"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestNewLoggerUsesLogLevel(t *testing.T) {
	for _, testCase := range []struct {
		levelName    string
		debugEnabled bool
		warning      bool
	}{
		{"", false, false}, {"debug", true, false}, {" warn ", false, false}, {"verbose", false, true},
	} {
		var output bytes.Buffer
		logger := newLogger(&output, testCase.levelName)
		require.Equal(t, testCase.debugEnabled, logger.Enabled(context.Background(), slog.LevelDebug), testCase.levelName)
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
	retries := logs.Find("dex server unavailable; retrying", nil)
	require.Len(t, retries, 2)
	for index, delay := range []string{"250ms", "500ms"} {
		require.Equal(t, map[string]string{"attempt": strconv.Itoa(index + 1), "delay": delay, "error": "connection refused"}, retries[index].Attrs)
	}
	require.Len(t, logs.Find("dex server available after retry", map[string]string{"attempts": "3"}), 1)
}

func TestWaitForDexServerStopsWhenContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- waitForDexServer(ctx, func(context.Context) (dex.HealthInfo, error) {
			cancel()
			return dex.HealthInfo{}, errors.New("connection refused")
		}, testlog.NewLogRecorder().Logger())
	}()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("waitForDexServer did not stop after cancellation")
	}
}
