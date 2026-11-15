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

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("FRESHDESK_TICKET_CHANGED_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("FRESHDESK_TICKET_CHANGED_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("FRESHDESK_TICKET_CHANGED_MISSING", "fallback"))
}

func TestConnectionOptionsRedirectOnlyWhenTheLocalVariableIsSet(t *testing.T) {
	var output bytes.Buffer
	logger := newLogger(&output, "info")
	t.Setenv(localAPIBaseURLEnvironmentVariable, "")
	require.Len(t, connectionOptions(logger), 1, "only the logger")
	t.Setenv(localAPIBaseURLEnvironmentVariable, "http://127.0.0.1:8899/api/v2")
	require.Len(t, connectionOptions(logger), 2)
	require.Contains(t, output.String(), localAPIBaseURLEnvironmentVariable)
	require.NotContains(t, output.String(), "127.0.0.1", "the warning names the variable, not its value")
}
