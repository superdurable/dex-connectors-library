// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package attemptcontext builds one attempt's dex.Context outside a Worker, mirroring
// dex.NewStandaloneContext until Dex Go SDK 1.41.0.
package attemptcontext

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
)

// Options mirrors the dex.StandaloneContextOptions fields of the same names.
type Options struct {
	// FlowID is returned by FlowID and RunID.
	FlowID string
	// StepExecutionID is returned by StepExecutionID. It stays the same for
	// every attempt of one logical call.
	StepExecutionID string
	// FirstAttemptAt is returned by FirstAttemptAt and FlowStartedAt.
	FirstAttemptAt time.Time
	// Attempt is the one-based attempt number; New rejects a value below 1.
	Attempt int32
	// LastHeartbeat is the JSON checkpoint as of the attempt start, which
	// GetLastHeartbeatValue decodes. Nil or JSON null means no checkpoint.
	LastHeartbeat json.RawMessage
	// OnRecordHeartbeat receives each checkpoint as JSON, nil when cleared,
	// before RecordHeartbeat returns. Nil discards checkpoints.
	OnRecordHeartbeat func(checkpoint json.RawMessage) error
}

// New follows dex.NewStandaloneContext: heartbeats reach only the next attempt,
// locals are discarded, and hooks stop once parent is done.
func New(parent context.Context, options Options) (dex.Context, error) {
	if parent == nil {
		panic("attemptcontext: New requires a non-nil parent context")
	}
	if options.Attempt < 1 {
		return nil, fmt.Errorf("attempt context: attempt must be at least 1, got %d", options.Attempt)
	}
	if len(options.LastHeartbeat) > 0 && !json.Valid(options.LastHeartbeat) {
		return nil, errors.New("attempt context: the last heartbeat is not JSON")
	}
	return &standaloneAttemptContext{Context: parent, options: options, recordedEventNames: map[string]struct{}{}}, nil
}

// standaloneAttemptContext is the dex.Context New returns.
type standaloneAttemptContext struct {
	context.Context
	options Options

	// stagingMu serializes hooks and guards recordedEventNames.
	stagingMu          sync.Mutex
	recordedEventNames map[string]struct{}
}

var _ dex.Context = (*standaloneAttemptContext)(nil)

func (attemptContext *standaloneAttemptContext) FlowID() string { return attemptContext.options.FlowID }

func (attemptContext *standaloneAttemptContext) RunID() string { return attemptContext.options.FlowID }

func (attemptContext *standaloneAttemptContext) FlowStartedAt() time.Time {
	return attemptContext.options.FirstAttemptAt
}

func (attemptContext *standaloneAttemptContext) StepExecutionID() string {
	return attemptContext.options.StepExecutionID
}

func (*standaloneAttemptContext) FromStepExecutionID() string { return "" }

func (*standaloneAttemptContext) RecoveryError() *dex.RecoveryErrorInfo { return nil }

func (attemptContext *standaloneAttemptContext) FirstAttemptAt() time.Time {
	return attemptContext.options.FirstAttemptAt
}

func (attemptContext *standaloneAttemptContext) Attempt() int32 {
	return attemptContext.options.Attempt
}

func (*standaloneAttemptContext) HasTimerFired() bool { return false }

func (*standaloneAttemptContext) HasTimerFiredByIndex(int) bool { return false }

func (*standaloneAttemptContext) WaitForMethodFailed() bool { return false }

func (attemptContext *standaloneAttemptContext) RecordHeartbeat(value any) error {
	var encoded json.RawMessage
	if !isNilValue(value) {
		var err error
		if encoded, err = json.Marshal(value); err != nil {
			return fmt.Errorf("attempt context: encode heartbeat: %w", err)
		}
	}
	attemptContext.stagingMu.Lock()
	defer attemptContext.stagingMu.Unlock()
	if err := attemptContext.requireParentNotDone(); err != nil {
		return err
	}
	if attemptContext.options.OnRecordHeartbeat == nil {
		return nil
	}
	if err := attemptContext.options.OnRecordHeartbeat(encoded); err != nil {
		return fmt.Errorf("attempt context: heartbeat hook: %w", err)
	}
	return nil
}

func (attemptContext *standaloneAttemptContext) GetLastHeartbeatValue(valuePtr any) (bool, error) {
	if err := requireDecodeTarget(valuePtr); err != nil {
		return false, err
	}
	checkpoint := attemptContext.options.LastHeartbeat
	if len(checkpoint) == 0 || string(checkpoint) == "null" {
		return false, nil
	}
	if err := json.Unmarshal(checkpoint, valuePtr); err != nil {
		return false, fmt.Errorf("attempt context: decode heartbeat: %w", err)
	}
	return true, nil
}

func (attemptContext *standaloneAttemptContext) SetStepExecutionLocal(key string, value any) error {
	if key == "" {
		return errors.New("attempt context: step execution local key must not be empty")
	}
	if _, err := json.Marshal(value); err != nil {
		return fmt.Errorf("attempt context: encode step execution local %q: %w", key, err)
	}
	attemptContext.stagingMu.Lock()
	defer attemptContext.stagingMu.Unlock()
	return attemptContext.requireParentNotDone()
}

func (*standaloneAttemptContext) GetStepExecutionLocal(key string, valuePtr any) (bool, error) {
	if key == "" {
		return false, errors.New("attempt context: step execution local key must not be empty")
	}
	return false, requireDecodeTarget(valuePtr)
}

func (attemptContext *standaloneAttemptContext) RecordEvent(name string, value any) error {
	if name == "" {
		return errors.New("attempt context: event name must not be empty")
	}
	if _, err := json.Marshal(value); err != nil {
		return fmt.Errorf("attempt context: encode event %q: %w", name, err)
	}
	attemptContext.stagingMu.Lock()
	defer attemptContext.stagingMu.Unlock()
	if _, isRecorded := attemptContext.recordedEventNames[name]; isRecorded {
		return fmt.Errorf("attempt context: event %q is already recorded", name)
	}
	if err := attemptContext.requireParentNotDone(); err != nil {
		return err
	}
	attemptContext.recordedEventNames[name] = struct{}{}
	return nil
}

func (attemptContext *standaloneAttemptContext) requireParentNotDone() error {
	if err := attemptContext.Err(); err != nil {
		return fmt.Errorf("attempt context: the attempt has ended: %w", context.Cause(attemptContext.Context))
	}
	return nil
}

func requireDecodeTarget(valuePtr any) error {
	reflected := reflect.ValueOf(valuePtr)
	if !reflected.IsValid() || reflected.Kind() != reflect.Pointer || reflected.IsNil() {
		return errors.New("attempt context: the decode target must be a non-nil pointer")
	}
	return nil
}

func isNilValue(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface, reflect.Func, reflect.Chan:
		return reflected.IsNil()
	default:
		return false
	}
}
