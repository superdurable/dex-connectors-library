// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import (
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
)

// DefaultConnectorStepExecuteRetryPolicy returns a fresh retry policy for Connector Step Execute.
// Attempts start one second apart, double their delay up to five minutes, and run for at most one hour.
// MaximumAttempts is zero, which means unlimited attempts within that total duration.
// Transient failures retry until the budget runs out, then the Flow fails.
// Applications can replace this policy per Step with StepOptionsOverride.ExecuteRetry.
func DefaultConnectorStepExecuteRetryPolicy() *dex.RetryPolicy {
	return &dex.RetryPolicy{
		InitialInterval:    time.Second,
		BackoffCoefficient: 2,
		MaximumInterval:    5 * time.Minute,
		MaximumAttempts:    0,
		TotalDuration:      time.Hour,
	}
}
