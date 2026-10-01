// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("XERO_APPROVED_ORDER_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("XERO_APPROVED_ORDER_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("XERO_APPROVED_ORDER_EXAMPLE_MISSING", "fallback"))
}

func TestConnectionOptionsRedirectOnlyWhenTheLocalVariableIsSet(t *testing.T) {
	t.Setenv(localProviderURLEnvironmentVariable, "")
	require.Empty(t, connectionOptions())
	t.Setenv(localProviderURLEnvironmentVariable, "http://127.0.0.1:8899")
	require.Len(t, connectionOptions(), 1)
}
