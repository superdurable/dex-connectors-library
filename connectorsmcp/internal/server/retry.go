// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"math"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// Clock is the time source of retry delays, budgets, and retry windows.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
	// Sleep waits for duration and reports true, or reports false when ctx
	// ends first.
	Sleep(ctx context.Context, duration time.Duration) bool
}

// SystemClock is the wall clock.
type SystemClock struct{}

// Now returns time.Now.
func (SystemClock) Now() time.Time { return time.Now() }

// Sleep waits for duration unless ctx ends first.
func (SystemClock) Sleep(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// retryPolicyOf returns the operation's Execute retry policy or the connector default.
func retryPolicyOf(metadata sdkgo.AgentToolMetadata) *dex.RetryPolicy {
	if policy := metadata.StepDefaults.ExecuteRetry; policy != nil {
		return policy
	}
	return sdkgo.DefaultConnectorStepExecuteRetryPolicy()
}

// retryDelay returns the delay before the attempt after completedAttempt:
// InitialInterval times BackoffCoefficient^(completedAttempt-1), capped at
// MaximumInterval, or the connector's RetryAfter when that is longer. Zero
// policy fields take the connector defaults.
func retryDelay(policy *dex.RetryPolicy, completedAttempt int32, retryAfter time.Duration) time.Duration {
	defaults := sdkgo.DefaultConnectorStepExecuteRetryPolicy()
	initial := policy.InitialInterval
	if initial <= 0 {
		initial = defaults.InitialInterval
	}
	coefficient := policy.BackoffCoefficient
	if coefficient < 1 {
		coefficient = defaults.BackoffCoefficient
	}
	maximum := policy.MaximumInterval
	if maximum <= 0 {
		maximum = defaults.MaximumInterval
	}
	delay := float64(initial) * math.Pow(coefficient, float64(max(completedAttempt-1, 0)))
	computed := maximum
	if delay < float64(maximum) {
		computed = time.Duration(delay)
	}
	return max(computed, retryAfter)
}

// attemptLimit is the inline attempt cap: maximumInlineAttempts, lowered by
// the policy's MaximumAttempts when that is set.
func attemptLimit(policy *dex.RetryPolicy, maximumInlineAttempts int32) int32 {
	if policy.MaximumAttempts > 0 && policy.MaximumAttempts < maximumInlineAttempts {
		return policy.MaximumAttempts
	}
	return maximumInlineAttempts
}

// retryWindow is how long after the first attempt a write may start another:
// the policy's TotalDuration, or one hour, and never beyond retention.
func retryWindow(policy *dex.RetryPolicy, retention time.Duration) time.Duration {
	window := policy.TotalDuration
	if window <= 0 {
		window = time.Hour
	}
	return min(window, retention)
}
