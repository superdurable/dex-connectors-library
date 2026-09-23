// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package testsupport

import (
	"context"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
)

// DexContext supplies deterministic Dex identity for connector unit tests.
type DexContext struct {
	context.Context
	Flow       string
	Run        string
	Step       string
	FromStep   string
	AttemptNo  int32
	StartedAt  time.Time
	FirstAt    time.Time
	Recovery   *dex.RecoveryErrorInfo
	TimerFired bool
	WaitFailed bool
}

func NewDexContext(flowID, stepExecutionID string) *DexContext {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return &DexContext{
		Context: context.Background(), Flow: flowID, Run: "run-1", Step: stepExecutionID,
		AttemptNo: 1, StartedAt: now, FirstAt: now,
	}
}

func (ctx *DexContext) FlowID() string                              { return ctx.Flow }
func (ctx *DexContext) RunID() string                               { return ctx.Run }
func (ctx *DexContext) FlowStartedAt() time.Time                    { return ctx.StartedAt }
func (ctx *DexContext) StepExecutionID() string                     { return ctx.Step }
func (ctx *DexContext) FromStepExecutionID() string                 { return ctx.FromStep }
func (ctx *DexContext) RecoveryError() *dex.RecoveryErrorInfo       { return ctx.Recovery }
func (ctx *DexContext) FirstAttemptAt() time.Time                   { return ctx.FirstAt }
func (ctx *DexContext) Attempt() int32                              { return ctx.AttemptNo }
func (ctx *DexContext) HasTimerFired() bool                         { return ctx.TimerFired }
func (ctx *DexContext) HasTimerFiredByIndex(index int) bool         { return ctx.TimerFired && index == 0 }
func (ctx *DexContext) WaitForMethodFailed() bool                   { return ctx.WaitFailed }
func (*DexContext) RecordHeartbeat(any) error                       { return nil }
func (*DexContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*DexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*DexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*DexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*DexContext)(nil)
