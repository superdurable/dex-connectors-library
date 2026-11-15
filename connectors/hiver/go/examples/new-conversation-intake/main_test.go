// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("HIVER_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("HIVER_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("HIVER_EXAMPLE_MISSING", "fallback"))
}

func TestConnectionOptionsRedirectOnlyWhenTheLocalVariableIsSet(t *testing.T) {
	var output bytes.Buffer
	logger := newLogger(&output, "")
	t.Setenv(localAPIBaseURLEnvironmentVariable, "")
	require.Len(t, connectionOptions(logger), 1, "only the logger")
	t.Setenv(localAPIBaseURLEnvironmentVariable, "http://127.0.0.1:8899/v1")
	require.Len(t, connectionOptions(logger), 2)
	require.Contains(t, output.String(), localAPIBaseURLEnvironmentVariable)
	require.NotContains(t, output.String(), "127.0.0.1", "the log names the variable, not its value")
}

func TestNewLoggerFallsBackToInfoForAnUnknownLevel(t *testing.T) {
	var output bytes.Buffer
	logger := newLogger(&output, "verbose")
	logger.Debug("hidden")
	require.Contains(t, output.String(), "LOG_LEVEL is not debug, info, warn, or error")
	require.NotContains(t, output.String(), "hidden")
}

func TestWaitForDexServerRetriesUntilHealthyAndStopsOnCancel(t *testing.T) {
	var output bytes.Buffer
	attempts := 0
	healthy := func(context.Context) (dex.HealthInfo, error) {
		attempts++
		if attempts < 2 {
			return dex.HealthInfo{}, errors.New("unavailable")
		}
		return dex.HealthInfo{}, nil
	}
	require.NoError(t, waitForDexServer(context.Background(), healthy, newLogger(&output, "")))
	require.Equal(t, 2, attempts)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	unavailable := func(context.Context) (dex.HealthInfo, error) { return dex.HealthInfo{}, errors.New("unavailable") }
	require.ErrorIs(t, waitForDexServer(ctx, unavailable, newLogger(&output, "")), context.DeadlineExceeded)
}
