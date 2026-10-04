// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver

import (
	"context"
	"sync"
	"time"
)

const (
	// rateLimitPenaltyIntervals is the first hold after a 429, in request intervals.
	rateLimitPenaltyIntervals = 4
	// maximumRateLimitHold bounds one hold, including a long Retry-After.
	maximumRateLimitHold = time.Minute
)

// requestPacer spaces request starts one interval apart; Hiver answers 429 above one request per second.
type requestPacer struct {
	interval time.Duration
	now      func() time.Time
	mutex    sync.Mutex
	nextSlot time.Time
	// heldUntil ends the latest 429 hold; rateLimitPenalty doubles per consecutive 429.
	heldUntil        time.Time
	rateLimitPenalty time.Duration
}

func newRequestPacer(interval time.Duration, now func() time.Time) *requestPacer {
	return &requestPacer{interval: interval, now: now}
}

// wait reserves the next free slot and sleeps until it; a cancelled wait forfeits the slot.
func (pacer *requestPacer) wait(ctx context.Context) error {
	for {
		delay := pacer.reserveSlot()
		if delay <= 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		} else if err := sleepUntilSlot(ctx, delay); err != nil {
			return err
		}
		// A 429 that arrived while this request waited holds it too.
		if !pacer.isHeld() {
			return nil
		}
	}
}

// holdAfterRateLimit delays every later request start, because Hiver warns that continuous 429 retries risk a block.
func (pacer *requestPacer) holdAfterRateLimit(retryAfter time.Duration) {
	pacer.mutex.Lock()
	defer pacer.mutex.Unlock()
	pacer.rateLimitPenalty = min(max(2*pacer.rateLimitPenalty, rateLimitPenaltyIntervals*pacer.interval), maximumRateLimitHold)
	resumeAt := pacer.now().Add(min(max(retryAfter, pacer.rateLimitPenalty), maximumRateLimitHold))
	if resumeAt.After(pacer.heldUntil) {
		pacer.heldUntil = resumeAt
	}
	if resumeAt.After(pacer.nextSlot) {
		pacer.nextSlot = resumeAt
	}
}

// resetRateLimitPenalty restarts the penalty once Hiver answers without a 429.
func (pacer *requestPacer) resetRateLimitPenalty() {
	pacer.mutex.Lock()
	defer pacer.mutex.Unlock()
	pacer.rateLimitPenalty = 0
}

func (pacer *requestPacer) reserveSlot() time.Duration {
	pacer.mutex.Lock()
	defer pacer.mutex.Unlock()
	now := pacer.now()
	slot := pacer.nextSlot
	if slot.Before(now) {
		slot = now
	}
	pacer.nextSlot = slot.Add(pacer.interval)
	return slot.Sub(now)
}

func (pacer *requestPacer) isHeld() bool {
	pacer.mutex.Lock()
	defer pacer.mutex.Unlock()
	return pacer.now().Before(pacer.heldUntil)
}

func sleepUntilSlot(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
