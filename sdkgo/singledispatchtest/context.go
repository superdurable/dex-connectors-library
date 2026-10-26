// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package singledispatchtest simulates Dex heartbeat loss across Step attempts.
package singledispatchtest

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
)

// Context is a fake Dex context for connector fence tests.
// It snapshots the last persisted heartbeat when an attempt advances.
type Context struct {
	context.Context
	mu                  sync.Mutex
	flowID              string
	stepExecutionID     string
	attempt             int32
	lastHeartbeat       []byte
	currentHeartbeat    []byte
	isLocalPhase        bool
	dropLocalHeartbeats bool
	loseNextHeartbeat   bool
	readError           error
	writeError          error
	heartbeats          [][]byte
}

// NewContext constructs an attempt-one context with no heartbeat checkpoint.
func NewContext(flowID, stepExecutionID string) *Context {
	return &Context{Context: context.Background(), flowID: flowID, stepExecutionID: stepExecutionID, attempt: 1}
}

// SetDeadline replaces the underlying attempt context with a bounded context.
// The returned cancel function belongs to the caller.
func (ctx *Context) SetDeadline(deadline time.Time) context.CancelFunc {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	bounded, cancel := context.WithDeadline(context.Background(), deadline)
	ctx.Context = bounded
	return cancel
}

// SetLocalPhase simulates Dex's initial local activity phase.
func (ctx *Context) SetLocalPhase(isLocalPhase bool) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.isLocalPhase = isLocalPhase
}

// DropLocalHeartbeats makes local-phase heartbeats invisible to later attempts.
func (ctx *Context) DropLocalHeartbeats() {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.dropLocalHeartbeats = true
}

// LoseNextHeartbeat discards one heartbeat frame while returning success.
func (ctx *Context) LoseNextHeartbeat() {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.loseNextHeartbeat = true
}

// SetReadError makes later heartbeat reads fail until cleared with nil.
func (ctx *Context) SetReadError(err error) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.readError = err
}

// SetWriteError makes later heartbeat records fail until cleared with nil.
func (ctx *Context) SetWriteError(err error) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.writeError = err
}

// SetPersistedCheckpoint seeds a heartbeat visible to the current attempt.
func (ctx *Context) SetPersistedCheckpoint(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.lastHeartbeat = encoded
	return nil
}

// AdvanceAttempt makes the last persisted heartbeat visible to the next attempt.
func (ctx *Context) AdvanceAttempt() {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.attempt++
	if ctx.currentHeartbeat != nil {
		ctx.lastHeartbeat = ctx.currentHeartbeat
	}
	ctx.currentHeartbeat = nil
	ctx.isLocalPhase = false
}

// Heartbeats returns the heartbeat frames in recording order.
func (ctx *Context) Heartbeats() [][]byte {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	frames := make([][]byte, len(ctx.heartbeats))
	for index, frame := range ctx.heartbeats {
		frames[index] = append([]byte(nil), frame...)
	}
	return frames
}

// FlowID returns the fake Flow ID.
func (ctx *Context) FlowID() string { return ctx.flowID }

// RunID returns a stable fake run ID.
func (*Context) RunID() string { return "run-1" }

// FlowStartedAt returns a stable fake start time.
func (*Context) FlowStartedAt() time.Time { return time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC) }

// StepExecutionID returns the fake Step execution ID.
func (ctx *Context) StepExecutionID() string { return ctx.stepExecutionID }

// FromStepExecutionID returns no predecessor for this fake Step.
func (*Context) FromStepExecutionID() string { return "" }

// RecoveryError returns no recovery state for this fake Step.
func (*Context) RecoveryError() *dex.RecoveryErrorInfo { return nil }

// FirstAttemptAt returns a stable fake first-attempt time.
func (*Context) FirstAttemptAt() time.Time { return time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC) }

// Attempt returns the current one-based attempt number.
func (ctx *Context) Attempt() int32 { ctx.mu.Lock(); defer ctx.mu.Unlock(); return ctx.attempt }

// HasTimerFired reports no timer for this fake Step.
func (*Context) HasTimerFired() bool { return false }

// HasTimerFiredByIndex reports no timer for this fake Step.
func (*Context) HasTimerFiredByIndex(int) bool { return false }

// WaitForMethodFailed reports no WaitFor failure for this fake Step.
func (*Context) WaitForMethodFailed() bool { return false }

// RecordHeartbeat persists a non-nil frame unless the configured loss mode discards it.
func (ctx *Context) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if ctx.writeError != nil {
		return ctx.writeError
	}
	ctx.heartbeats = append(ctx.heartbeats, encoded)
	if ctx.loseNextHeartbeat {
		ctx.loseNextHeartbeat = false
		return nil
	}
	if ctx.isLocalPhase && ctx.dropLocalHeartbeats {
		return nil
	}
	ctx.currentHeartbeat = encoded
	return nil
}

// GetLastHeartbeatValue decodes the checkpoint visible at attempt start.
func (ctx *Context) GetLastHeartbeatValue(value any) (bool, error) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if ctx.readError != nil {
		return false, ctx.readError
	}
	if len(ctx.lastHeartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(ctx.lastHeartbeat, value)
}

// SetStepExecutionLocal accepts a fake local value without persisting it.
func (*Context) SetStepExecutionLocal(string, any) error { return nil }

// GetStepExecutionLocal reports no local value.
func (*Context) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }

// RecordEvent accepts a fake diagnostic event.
func (*Context) RecordEvent(string, any) error { return nil }

var _ dex.Context = (*Context)(nil)
