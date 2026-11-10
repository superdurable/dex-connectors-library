// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import (
	"encoding/json"
	"reflect"
	"sync"

	"github.com/superdurable/dex/sdk-go/dex"
)

// singleDispatchPackagePath identifies singledispatch.Marker, which sdkgo cannot import without a cycle.
const singleDispatchPackagePath = "github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"

// singleDispatchCheckpoint mirrors the persisted JSON of singledispatch.Marker.
type singleDispatchCheckpoint struct {
	CallID     CallID `json:"callId"`
	Attempt    int32  `json:"attempt"`
	State      string `json:"state"`
	ProviderID string `json:"providerId,omitempty"`
}

const (
	singleDispatchStateNotSent   = "notSent"
	singleDispatchStateClaimed   = "claimed"
	singleDispatchStateCanResend = "canResend"
)

// dispatchObservingContext watches fence checkpoints of one Mutation attempt; it cannot serve SubFlow reads or text Streams.
type dispatchObservingContext struct {
	dex.Context
	callID CallID

	mu                       sync.Mutex
	isCheckpointUntrusted    bool
	hasPriorPossibleDispatch bool
	hasRecordedCheckpoint    bool
	lastRecordedCheckpoint   singleDispatchCheckpoint
}

func newDispatchObservingContext(ctx dex.Context, operation OperationRef) *dispatchObservingContext {
	return &dispatchObservingContext{Context: ctx, callID: deriveCallID(ctx.FlowID(), ctx.StepExecutionID(), operation)}
}

// RecordHeartbeat records the heartbeat and remembers a successfully recorded fence checkpoint.
func (ctx *dispatchObservingContext) RecordHeartbeat(value any) error {
	err := ctx.Context.RecordHeartbeat(value)
	if err != nil {
		return err
	}
	checkpoint, isCheckpoint, isReadable := singleDispatchCheckpointFrom(value)
	if !isCheckpoint {
		return nil
	}
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if !isReadable {
		ctx.isCheckpointUntrusted = true
		return nil
	}
	ctx.hasRecordedCheckpoint = true
	ctx.lastRecordedCheckpoint = checkpoint
	return nil
}

// GetLastHeartbeatValue reads the prior heartbeat, applying the fence's send rule to a fence checkpoint.
func (ctx *dispatchObservingContext) GetLastHeartbeatValue(value any) (bool, error) {
	hasPrevious, err := ctx.Context.GetLastHeartbeatValue(value)
	if !isSingleDispatchCheckpointType(value) {
		return hasPrevious, err
	}
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	switch {
	case err != nil:
		ctx.isCheckpointUntrusted = true
	case !hasPrevious:
		// A lost checkpoint after attempt one may hide an earlier send.
		ctx.hasPriorPossibleDispatch = ctx.Context.Attempt() > 1
	default:
		previous, _, isReadable := singleDispatchCheckpointFrom(value)
		if !isReadable || previous.CallID != ctx.callID || previous.Attempt < 1 || !isKnownSingleDispatchState(previous.State) {
			ctx.isCheckpointUntrusted = true
			return hasPrevious, err
		}
		isSendAllowed := previous.Attempt == ctx.Context.Attempt()-1 && previous.ProviderID == "" &&
			(previous.State == singleDispatchStateNotSent || previous.State == singleDispatchStateCanResend)
		ctx.hasPriorPossibleDispatch = !isSendAllowed
	}
	return hasPrevious, err
}

// mayHaveDispatched reports a fence checkpoint that cannot prove no request reached the provider.
func (ctx *dispatchObservingContext) mayHaveDispatched() bool {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	isClaimed := ctx.hasRecordedCheckpoint && ctx.lastRecordedCheckpoint.State == singleDispatchStateClaimed
	return ctx.isCheckpointUntrusted || ctx.hasPriorPossibleDispatch || isClaimed
}

// singleDispatchCheckpointFrom decodes a singledispatch.Marker value or pointer through its persisted JSON.
func singleDispatchCheckpointFrom(value any) (checkpoint singleDispatchCheckpoint, isCheckpoint bool, isReadable bool) {
	if !isSingleDispatchCheckpointType(value) {
		return singleDispatchCheckpoint{}, false, false
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return singleDispatchCheckpoint{}, true, false
	}
	if err := json.Unmarshal(encoded, &checkpoint); err != nil {
		return singleDispatchCheckpoint{}, true, false
	}
	return checkpoint, true, true
}

func isSingleDispatchCheckpointType(value any) bool {
	valueType := reflect.TypeOf(value)
	for valueType != nil && valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	return valueType != nil && valueType.PkgPath() == singleDispatchPackagePath && valueType.Name() == "Marker"
}

func isKnownSingleDispatchState(state string) bool {
	return state == singleDispatchStateNotSent || state == singleDispatchStateClaimed || state == singleDispatchStateCanResend
}
