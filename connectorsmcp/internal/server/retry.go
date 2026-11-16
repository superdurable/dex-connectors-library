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

// retryDelay is exponential backoff, capped, or RetryAfter when longer.
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

// pinnedSendCap is the send cap one write call passes on every attempt: the
// manifest MaxSends of a limited Mutation, 1 for a limited tool without a
// usable cap, and 0, meaning no send-cap option, for every other tool.
func pinnedSendCap(metadata sdkgo.AgentToolMetadata) int32 {
	if metadata.Kind != sdkgo.AgentOperationKindMutation || metadata.RepeatOnUnknown != sdkgo.RepeatOnUnknownLimited {
		return 0
	}
	return min(max(metadata.MaxSends, 1), sdkgo.MaxSendsCeiling)
}

// allowsBoundedResend reports whether a pinned send cap lets a non-idempotent
// write run another attempt after a Retry.
func allowsBoundedResend(sendCap int32) bool { return sendCap >= 2 }

// sendCapRunOptions passes a limited tool's pinned cap explicitly, so that
// RunAttempt never falls back to a cap the host did not choose. WithMaxSends
// on any other tool is an error that would refuse the write.
func sendCapRunOptions(sendCap int32) []sdkgo.RunOption {
	if sendCap == 0 {
		return nil
	}
	return []sdkgo.RunOption{sdkgo.WithMaxSends(sendCap)}
}

// writeAttemptLimit is attemptLimit with the inline maximum raised by one
// attempt per extra send of a bounded resend; a policy's MaximumAttempts
// still lowers it, as the SDK keeps a definition policy at the definition cap.
func writeAttemptLimit(policy *dex.RetryPolicy, maximumInlineAttempts int32, sendCap int32) int32 {
	if !allowsBoundedResend(sendCap) {
		return attemptLimit(policy, maximumInlineAttempts)
	}
	return attemptLimit(policy, maximumInlineAttempts+sendCap-1)
}

// retryWindow is TotalDuration, or one hour, capped at retention.
func retryWindow(policy *dex.RetryPolicy, retention time.Duration) time.Duration {
	window := policy.TotalDuration
	if window <= 0 {
		window = time.Hour
	}
	return min(window, retention)
}
