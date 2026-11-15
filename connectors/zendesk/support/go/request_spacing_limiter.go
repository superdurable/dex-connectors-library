// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support

import (
	"context"
	"sync"
	"time"
)

// requestSpacingLimiter starts at most one request per spacing, in arrival order, so a burst of polls stays
// inside Zendesk's per-minute incremental export limit instead of drawing a 429. It is safe for concurrent use.
type requestSpacingLimiter struct {
	spacing   time.Duration
	mutex     sync.Mutex
	nextStart time.Time
}

func newRequestSpacingLimiter(spacing time.Duration) *requestSpacingLimiter {
	return &requestSpacingLimiter{spacing: spacing}
}

// waitForRequestStart blocks until the caller's reserved start time, or returns ctx.Err() when ctx ends
// first. A cancelled wait keeps its reservation, which only delays the next request.
func (limiter *requestSpacingLimiter) waitForRequestStart(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	limiter.mutex.Lock()
	now := time.Now()
	start := now
	if limiter.nextStart.After(now) {
		start = limiter.nextStart
	}
	limiter.nextStart = start.Add(limiter.spacing)
	limiter.mutex.Unlock()
	delay := start.Sub(now)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
