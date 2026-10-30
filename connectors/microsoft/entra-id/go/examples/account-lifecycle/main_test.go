// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("ENTRA_ID_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("ENTRA_ID_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("ENTRA_ID_EXAMPLE_MISSING", "fallback"))
}

func TestConnectorOptionsRouteToALocalFakeOnlyWhenAsked(t *testing.T) {
	t.Setenv(localProviderURLEnvironmentVariable, "")
	require.Empty(t, connectorOptions())
	t.Setenv(localProviderURLEnvironmentVariable, "http://127.0.0.1:8930")
	require.Len(t, connectorOptions(), 1)
}
