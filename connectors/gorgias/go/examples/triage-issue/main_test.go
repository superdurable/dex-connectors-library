// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("GORGIAS_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("GORGIAS_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("GORGIAS_EXAMPLE_MISSING", "fallback"))
}

func TestConnectionOptionsRedirectOnlyWhenTheLocalVariableIsSet(t *testing.T) {
	t.Setenv(localAPIBaseURLEnvironmentVariable, "")
	require.Empty(t, connectionOptions())
	t.Setenv(localAPIBaseURLEnvironmentVariable, "http://127.0.0.1:8899/api")
	require.Len(t, connectionOptions(), 1)
}
