// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("NEWSAPI_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("NEWSAPI_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("NEWSAPI_EXAMPLE_MISSING", "fallback"))
}

func TestRunRequiresTheProjectEnvironment(t *testing.T) {
	t.Setenv("DEX_PROJECT_ID", "")
	require.Error(t, run(context.Background(), slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))))
}

func TestWaitForDexServerRetriesUntilTheServerAnswers(t *testing.T) {
	var output bytes.Buffer
	attempts := 0
	err := waitForDexServer(context.Background(), func(context.Context) (dex.HealthInfo, error) {
		attempts++
		if attempts < 3 {
			return dex.HealthInfo{}, errors.New("connection refused")
		}
		return dex.HealthInfo{Condition: "SERVING"}, nil
	}, slog.New(slog.NewTextHandler(&output, nil)))
	require.NoError(t, err)
	require.Equal(t, 3, attempts)
	require.Equal(t, 2, strings.Count(output.String(), `msg="dex server unavailable; retrying"`))
	require.Contains(t, output.String(), "attempt=2 delay=500ms")
}

func TestWaitForDexServerStopsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := waitForDexServer(ctx, func(context.Context) (dex.HealthInfo, error) {
		return dex.HealthInfo{}, errors.New("connection refused")
	}, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	require.ErrorIs(t, err, context.Canceled)
}
