// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRateLimitHoldGrowsUntilHiverAnswersWithoutA429(t *testing.T) {
	start := time.Unix(1_000, 0)
	now := start
	pacer := newRequestPacer(time.Second, func() time.Time { return now })
	require.Zero(t, pacer.reserveSlot())
	require.Equal(t, time.Second, pacer.reserveSlot(), "a second Step waits one interval")

	pacer.holdAfterRateLimit(0)
	now = start.Add(time.Second)
	require.True(t, pacer.isHeld(), "the Step that reserved before the 429 is held when it wakes")
	require.Equal(t, 3*time.Second, pacer.reserveSlot(), "the first hold is four intervals")

	pacer.holdAfterRateLimit(0)
	require.Equal(t, start.Add(9*time.Second), pacer.heldUntil, "a further 429 doubles the penalty")

	pacer.resetRateLimitPenalty()
	now = start.Add(20 * time.Second)
	pacer.holdAfterRateLimit(0)
	require.Equal(t, now.Add(4*time.Second), pacer.heldUntil, "any other response restarts the penalty")

	pacer.holdAfterRateLimit(10 * time.Second)
	require.Equal(t, now.Add(10*time.Second), pacer.heldUntil, "Retry-After wins over a shorter penalty")

	pacer.holdAfterRateLimit(time.Hour)
	require.Equal(t, now.Add(maximumRateLimitHold), pacer.heldUntil, "no hold exceeds one minute")
	for range 10 {
		pacer.holdAfterRateLimit(0)
	}
	require.Equal(t, maximumRateLimitHold, pacer.rateLimitPenalty)
	now = now.Add(maximumRateLimitHold)
	require.False(t, pacer.isHeld())
}
