// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("SNOWFLAKE_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("SNOWFLAKE_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("SNOWFLAKE_EXAMPLE_MISSING", "fallback"))
}
