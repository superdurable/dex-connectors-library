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
	t.Setenv("XERO_INVOICE_CHANGED_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("XERO_INVOICE_CHANGED_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("XERO_INVOICE_CHANGED_MISSING", "fallback"))
}

func TestConnectionOptionsAddTheLocalProviderOnlyWhenSet(t *testing.T) {
	var output bytes.Buffer
	logger := newLogger(&output, "info")
	t.Setenv(localProviderURLEnvironmentVariable, "")
	require.Len(t, connectionOptions(logger), 1)
	t.Setenv(localProviderURLEnvironmentVariable, "http://127.0.0.1:9")
	require.Len(t, connectionOptions(logger), 2)
	require.Contains(t, output.String(), localProviderURLEnvironmentVariable)
}
