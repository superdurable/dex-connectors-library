// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package testsupport

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
)

type DexContext struct {
	context.Context
	Flow, Run, Step, FromStep string
	AttemptNo                 int32
	StartedAt, FirstAt        time.Time
	Recovery                  *dex.RecoveryErrorInfo
	TimerFired, WaitFailed    bool
	heartbeatMu               sync.Mutex
	heartbeatValue            []byte
}

func NewDexContext(flowID, stepExecutionID string) *DexContext {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return &DexContext{Context: context.Background(), Flow: flowID, Run: "run-1", Step: stepExecutionID, AttemptNo: 1, StartedAt: now, FirstAt: now}
}

func (ctx *DexContext) FlowID() string                        { return ctx.Flow }
func (ctx *DexContext) RunID() string                         { return ctx.Run }
func (ctx *DexContext) FlowStartedAt() time.Time              { return ctx.StartedAt }
func (ctx *DexContext) StepExecutionID() string               { return ctx.Step }
func (ctx *DexContext) FromStepExecutionID() string           { return ctx.FromStep }
func (ctx *DexContext) RecoveryError() *dex.RecoveryErrorInfo { return ctx.Recovery }
func (ctx *DexContext) FirstAttemptAt() time.Time             { return ctx.FirstAt }
func (ctx *DexContext) Attempt() int32                        { return ctx.AttemptNo }
func (ctx *DexContext) HasTimerFired() bool                   { return ctx.TimerFired }
func (ctx *DexContext) HasTimerFiredByIndex(index int) bool   { return ctx.TimerFired && index == 0 }
func (ctx *DexContext) WaitForMethodFailed() bool             { return ctx.WaitFailed }
func (ctx *DexContext) RecordHeartbeat(value any) error {
	ctx.heartbeatMu.Lock()
	defer ctx.heartbeatMu.Unlock()
	if value == nil {
		ctx.heartbeatValue = nil
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	ctx.heartbeatValue = encoded
	return nil
}
func (ctx *DexContext) GetLastHeartbeatValue(value any) (bool, error) {
	ctx.heartbeatMu.Lock()
	defer ctx.heartbeatMu.Unlock()
	if len(ctx.heartbeatValue) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(ctx.heartbeatValue, value)
}
func (*DexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*DexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*DexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*DexContext)(nil)
