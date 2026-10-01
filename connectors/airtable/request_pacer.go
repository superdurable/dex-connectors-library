// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable

import (
	"context"
	"sync"
	"time"
)

const (
	// requestIntervalPerBase spaces requests to one base at Airtable's limit of 5 requests per second per base.
	requestIntervalPerBase = time.Second / 5
	// maximumPacingWait is the longest in-Step wait; a longer queue returns Retry so Dex reschedules the Step.
	maximumPacingWait = 2 * time.Second
	// rateLimitCooldown is the wait Airtable documents after a 429 before requests succeed again.
	rateLimitCooldown = 30 * time.Second
	// prunedPacerEntryThreshold starts removing idle bases once the pacer tracks this many.
	prunedPacerEntryThreshold = 256
)

// baseRequestPacer spaces and, after a 429, holds one process's requests to each base.
type baseRequestPacer struct {
	mutex         sync.Mutex
	now           func() time.Time
	nextRequestAt map[string]time.Time
}

func newBaseRequestPacer(now func() time.Time) *baseRequestPacer {
	return &baseRequestPacer{now: now, nextRequestAt: map[string]time.Time{}}
}

// waitForRequestSlot waits for baseID's next slot, or returns a too-distant slot's delay for Retry.
func (pacer *baseRequestPacer) waitForRequestSlot(ctx context.Context, baseID string) (time.Duration, error) {
	delay, isReserved := pacer.reserveRequestSlot(baseID)
	if !isReserved {
		return delay, nil
	}
	if delay <= 0 {
		return 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-timer.C:
		return 0, nil
	}
}

func (pacer *baseRequestPacer) reserveRequestSlot(baseID string) (time.Duration, bool) {
	pacer.mutex.Lock()
	defer pacer.mutex.Unlock()
	now := pacer.now()
	pacer.pruneIdleBases(now)
	slot := now
	if next, exists := pacer.nextRequestAt[baseID]; exists && next.After(now) {
		slot = next
	}
	delay := slot.Sub(now)
	if delay > maximumPacingWait {
		return delay, false
	}
	pacer.nextRequestAt[baseID] = slot.Add(requestIntervalPerBase)
	return delay, true
}

// holdAfterRateLimit delays every later request to baseID until Airtable's cooldown has passed.
func (pacer *baseRequestPacer) holdAfterRateLimit(baseID string, cooldown time.Duration) {
	pacer.mutex.Lock()
	defer pacer.mutex.Unlock()
	resumeAt := pacer.now().Add(cooldown)
	if next, exists := pacer.nextRequestAt[baseID]; !exists || resumeAt.After(next) {
		pacer.nextRequestAt[baseID] = resumeAt
	}
}

// pruneIdleBases removes bases whose next slot has passed once the map is large; the caller holds the mutex.
func (pacer *baseRequestPacer) pruneIdleBases(now time.Time) {
	if len(pacer.nextRequestAt) < prunedPacerEntryThreshold {
		return
	}
	for baseID, next := range pacer.nextRequestAt {
		if !next.After(now) {
			delete(pacer.nextRequestAt, baseID)
		}
	}
}
