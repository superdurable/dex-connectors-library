// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("SQL_SERVER_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("SQL_SERVER_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("SQL_SERVER_EXAMPLE_MISSING", "fallback"))
}
