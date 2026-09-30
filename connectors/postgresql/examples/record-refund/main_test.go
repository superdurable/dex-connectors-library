// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("POSTGRESQL_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("POSTGRESQL_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("POSTGRESQL_EXAMPLE_MISSING", "fallback"))
}
