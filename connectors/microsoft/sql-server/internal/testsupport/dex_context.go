// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package testsupport provides a Step-execution dex.Context for connector tests without a Dex Server.
package testsupport

import (
	"context"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
)

// DexContext is a fixed Step-execution identity that satisfies dex.Context.
type DexContext struct {
	context.Context
	// Flow is the returned Flow ID.
	Flow string
	// Step is the returned Step execution ID.
	Step string
	// AttemptNumber is the returned one-based attempt.
	AttemptNumber int32
}

// NewDexContext returns a first-attempt context for one Flow and Step execution.
func NewDexContext(flowID string, stepExecutionID string) *DexContext {
	return &DexContext{Context: context.Background(), Flow: flowID, Step: stepExecutionID, AttemptNumber: 1}
}

var fixedTime = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

// FlowID returns Flow.
func (ctx *DexContext) FlowID() string { return ctx.Flow }

// RunID returns a fixed run ID.
func (*DexContext) RunID() string { return "run-1" }

// FlowStartedAt returns a fixed time.
func (*DexContext) FlowStartedAt() time.Time { return fixedTime }

// StepExecutionID returns Step.
func (ctx *DexContext) StepExecutionID() string { return ctx.Step }

// FromStepExecutionID returns no predecessor.
func (*DexContext) FromStepExecutionID() string { return "" }

// RecoveryError returns no recovery failure.
func (*DexContext) RecoveryError() *dex.RecoveryErrorInfo { return nil }

// FirstAttemptAt returns a fixed time.
func (*DexContext) FirstAttemptAt() time.Time { return fixedTime }

// Attempt returns AttemptNumber.
func (ctx *DexContext) Attempt() int32 { return ctx.AttemptNumber }

// HasTimerFired reports no Timer.
func (*DexContext) HasTimerFired() bool { return false }

// HasTimerFiredByIndex reports no Timer.
func (*DexContext) HasTimerFiredByIndex(int) bool { return false }

// WaitForMethodFailed reports no WaitFor failure.
func (*DexContext) WaitForMethodFailed() bool { return false }

// RecordHeartbeat discards the heartbeat.
func (*DexContext) RecordHeartbeat(any) error { return nil }

// GetLastHeartbeatValue reports no heartbeat.
func (*DexContext) GetLastHeartbeatValue(any) (bool, error) { return false, nil }

// SetStepExecutionLocal discards the value.
func (*DexContext) SetStepExecutionLocal(string, any) error { return nil }

// GetStepExecutionLocal reports no value.
func (*DexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }

// RecordEvent discards the event.
func (*DexContext) RecordEvent(string, any) error { return nil }

var _ dex.Context = (*DexContext)(nil)
