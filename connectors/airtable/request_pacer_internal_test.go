// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// manualClock is a test clock that moves only when the test advances it.
type manualClock struct {
	mutex sync.Mutex
	now   time.Time
}

func (clock *manualClock) read() time.Time {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	return clock.now
}

func (clock *manualClock) advance(duration time.Duration) {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	clock.now = clock.now.Add(duration)
}

func TestPacerSpacesRequestsToOneBaseAndDefersALongQueue(t *testing.T) {
	clock := &manualClock{now: time.Unix(1_000, 0)}
	pacer := newBaseRequestPacer(clock.read)
	var delays []time.Duration
	for range 11 {
		delay, isReserved := pacer.reserveRequestSlot("appRefundBase0001")
		require.True(t, isReserved)
		delays = append(delays, delay)
	}
	require.Equal(t, time.Duration(0), delays[0])
	require.Equal(t, 200*time.Millisecond, delays[1])
	require.Equal(t, maximumPacingWait, delays[10], "the eleventh request waits two seconds")
	delay, isReserved := pacer.reserveRequestSlot("appRefundBase0001")
	require.False(t, isReserved, "a longer queue returns Retry instead of waiting in the Step")
	require.Equal(t, 2200*time.Millisecond, delay)

	delay, isReserved = pacer.reserveRequestSlot("appOtherBase00001")
	require.True(t, isReserved)
	require.Zero(t, delay, "each base has its own budget")

	clock.advance(3 * time.Second)
	delay, isReserved = pacer.reserveRequestSlot("appRefundBase0001")
	require.True(t, isReserved)
	require.Zero(t, delay)
}

func TestPacerHoldsABaseForTheRateLimitCooldown(t *testing.T) {
	clock := &manualClock{now: time.Unix(1_000, 0)}
	pacer := newBaseRequestPacer(clock.read)
	pacer.holdAfterRateLimit("appRefundBase0001", rateLimitCooldown)
	delay, isReserved := pacer.reserveRequestSlot("appRefundBase0001")
	require.False(t, isReserved)
	require.Equal(t, rateLimitCooldown, delay)

	clock.advance(rateLimitCooldown - time.Second)
	delay, isReserved = pacer.reserveRequestSlot("appRefundBase0001")
	require.True(t, isReserved, "the last second of the cooldown is waited in the Step")
	require.Equal(t, time.Second, delay)
}

func TestPacerWaitStopsWithTheStepContext(t *testing.T) {
	pacer := newBaseRequestPacer(time.Now)
	_, err := pacer.waitForRequestSlot(context.Background(), "appRefundBase0001")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = pacer.waitForRequestSlot(ctx, "appRefundBase0001")
	require.ErrorIs(t, err, context.Canceled)
}

func TestPacerForgetsIdleBasesOnceItTracksMany(t *testing.T) {
	clock := &manualClock{now: time.Unix(1_000, 0)}
	pacer := newBaseRequestPacer(clock.read)
	for index := range prunedPacerEntryThreshold {
		pacer.reserveRequestSlot("app" + strconv.Itoa(index))
	}
	clock.advance(time.Second)
	pacer.reserveRequestSlot("appNewBase0000001")
	require.Len(t, pacer.nextRequestAt, 1)
}
