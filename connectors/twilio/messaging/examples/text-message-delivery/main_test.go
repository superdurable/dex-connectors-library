// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("TWILIO_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("TWILIO_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("TWILIO_EXAMPLE_MISSING", "fallback"))
}
