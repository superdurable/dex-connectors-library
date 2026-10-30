// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday

import (
	"context"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const mutationHeartbeatInterval = 5 * time.Second

// mutationCheckpoint permits a resend only after the immediately preceding attempt proved it safe.
type mutationCheckpoint struct {
	CallID    sdkgo.CallID `json:"callId"`
	Attempt   int32        `json:"attempt"`
	CanResend bool         `json:"canResend,omitempty"`
}

type mutationHeartbeat struct {
	call       sdkgo.Call
	mutex      sync.Mutex
	checkpoint mutationCheckpoint
	stopSignal chan struct{}
	stopped    chan struct{}
}

func newMutationHeartbeat(call sdkgo.Call) *mutationHeartbeat {
	return &mutationHeartbeat{call: call}
}

func (heartbeat *mutationHeartbeat) begin() error {
	if err := heartbeat.record(mutationCheckpoint{CallID: heartbeat.call.ID, Attempt: heartbeat.call.Context.Attempt()}); err != nil {
		return err
	}
	heartbeat.stopSignal = make(chan struct{})
	heartbeat.stopped = make(chan struct{})
	go heartbeat.recordUntilStopped()
	return nil
}

func (heartbeat *mutationHeartbeat) recordCanResend() error {
	return heartbeat.record(mutationCheckpoint{CallID: heartbeat.call.ID, Attempt: heartbeat.call.Context.Attempt(), CanResend: true})
}

func (heartbeat *mutationHeartbeat) record(checkpoint mutationCheckpoint) error {
	heartbeat.mutex.Lock()
	defer heartbeat.mutex.Unlock()
	heartbeat.checkpoint = checkpoint
	return heartbeat.call.Context.RecordHeartbeat(checkpoint)
}

func (heartbeat *mutationHeartbeat) recordUntilStopped() {
	defer close(heartbeat.stopped)
	ticker := time.NewTicker(mutationHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-heartbeat.stopSignal:
			return
		case <-heartbeat.call.Context.Done():
			return
		case <-ticker.C:
			heartbeat.mutex.Lock()
			_ = heartbeat.call.Context.RecordHeartbeat(heartbeat.checkpoint)
			heartbeat.mutex.Unlock()
		}
	}
}

func (heartbeat *mutationHeartbeat) stop() {
	if heartbeat.stopSignal != nil {
		close(heartbeat.stopSignal)
		<-heartbeat.stopped
	}
}

func hasRequestBudget(ctx context.Context, minimum time.Duration) bool {
	deadline, hasDeadline := ctx.Deadline()
	return !hasDeadline || time.Until(deadline) >= minimum
}
