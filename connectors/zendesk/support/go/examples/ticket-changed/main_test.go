// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("ZENDESK_SUPPORT_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("ZENDESK_SUPPORT_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("ZENDESK_SUPPORT_EXAMPLE_MISSING", "fallback"))
}

func TestNewLoggerFallsBackToInfoForAnUnknownLevel(t *testing.T) {
	var output bytes.Buffer
	logger := newLogger(&output, "verbose")
	require.Contains(t, output.String(), "LOG_LEVEL is not debug, info, warn, or error; using info")
	require.False(t, logger.Enabled(context.Background(), slog.LevelDebug))
	require.True(t, newLogger(&output, "debug").Enabled(context.Background(), slog.LevelDebug))
}

func TestWaitForDexServerRetriesUntilHealthy(t *testing.T) {
	var output bytes.Buffer
	attempts := 0
	err := waitForDexServer(context.Background(), func(context.Context) (dex.HealthInfo, error) {
		attempts++
		if attempts < 2 {
			return dex.HealthInfo{}, errors.New("connection refused")
		}
		return dex.HealthInfo{}, nil
	}, newLogger(&output, ""))
	require.NoError(t, err)
	require.Equal(t, 2, attempts)
	require.Contains(t, output.String(), "dex server unavailable; retrying")
	require.Contains(t, output.String(), "dex server available after retry")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, waitForDexServer(ctx, func(context.Context) (dex.HealthInfo, error) {
		return dex.HealthInfo{}, errors.New("connection refused")
	}, newLogger(&output, "")), context.Canceled)
}

func TestConnectionOptionsRedirectOnlyWhenTheLocalVariableIsSet(t *testing.T) {
	logger := newLogger(&bytes.Buffer{}, "")
	t.Setenv(localAPIBaseURLEnvironmentVariable, "")
	require.Len(t, connectionOptions(logger), 1, "only the logger option")
	t.Setenv(localAPIBaseURLEnvironmentVariable, "http://127.0.0.1:8899/api/v2")
	require.Len(t, connectionOptions(logger), 2)
}
