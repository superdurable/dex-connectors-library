// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("GOOGLE_DRIVE_FILE_INTAKE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("GOOGLE_DRIVE_FILE_INTAKE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("GOOGLE_DRIVE_FILE_INTAKE_MISSING", "fallback"))
}
