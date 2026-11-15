// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewLoggerDefaultsToInfoAndWarnsOnAnUnknownLevel(t *testing.T) {
	var output bytes.Buffer
	logger := newLogger(&output, "verbose")
	logger.Debug("hidden")
	require.Contains(t, output.String(), "LOG_LEVEL is not debug, info, warn, or error")
	require.NotContains(t, output.String(), "hidden")

	output.Reset()
	newLogger(&output, "debug").Debug("shown")
	require.Contains(t, output.String(), "shown")
}

func TestConnectionOptionsRedirectOnlyWithTheLocalVariable(t *testing.T) {
	var output bytes.Buffer
	t.Setenv(localAPIBaseURLEnvironmentVariable, "")
	require.Len(t, connectionOptions(newLogger(&output, "info")), 1)
	require.Empty(t, output.String())
	t.Setenv(localAPIBaseURLEnvironmentVariable, "http://127.0.0.1:9/api/v1")
	require.Len(t, connectionOptions(newLogger(&output, "info")), 2)
	require.Contains(t, output.String(), "local base URL")
	require.NotContains(t, output.String(), "127.0.0.1", "the redirect target is not logged")
}

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("REAMAZE_NEW_CONVERSATION_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("REAMAZE_NEW_CONVERSATION_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("REAMAZE_NEW_CONVERSATION_MISSING", "fallback"))
}
