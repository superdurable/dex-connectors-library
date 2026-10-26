// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import (
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
)

// DefaultConnectorStepExecuteRetryPolicy returns a fresh retry policy for Connector Step Execute.
// Generic factories use it when a Query or Mutation definition has nil StepDefaults.ExecuteRetry.
// Attempts start one second apart, double their delay up to five minutes, and run for at most one hour.
// MaximumAttempts is zero, which means unlimited attempts within that total duration.
// TotalDuration also bounds the first attempt, so longer Execute timeouts need an explicit policy.
// Exhausted retries fail the Flow unless ExecuteFailure routes to a recovery Step.
// StepOptionsOverride.ExecuteRetry replaces the whole policy; zero fields use Dex server defaults.
// To customize it, start with this fresh policy and change the needed fields.
func DefaultConnectorStepExecuteRetryPolicy() *dex.RetryPolicy {
	return &dex.RetryPolicy{
		InitialInterval:    time.Second,
		BackoffCoefficient: 2,
		MaximumInterval:    5 * time.Minute,
		MaximumAttempts:    0,
		TotalDuration:      time.Hour,
	}
}
