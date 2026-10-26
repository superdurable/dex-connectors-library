// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana

import (
	"errors"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const unkeyedWriteHeartbeatInterval = 5 * time.Second

var errForeignCheckpoint = errors.New("the Step heartbeat belongs to another connector call")

type unkeyedWriteCheckpoint struct {
	CallID    sdkgo.CallID `json:"callId"`
	Operation string       `json:"operation"`
	Attempt   int32        `json:"attempt"`
	ObjectID  string       `json:"objectId,omitempty"`
	CanResend bool         `json:"canResend,omitempty"`
}

type unkeyedWriteHeartbeat struct {
	call       sdkgo.Call
	operation  string
	mutex      sync.Mutex
	checkpoint unkeyedWriteCheckpoint
	stopSignal chan struct{}
	stopped    chan struct{}
}

func newUnkeyedWriteHeartbeat(call sdkgo.Call, operation string) *unkeyedWriteHeartbeat {
	return &unkeyedWriteHeartbeat{call: call, operation: operation}
}

func (heartbeat *unkeyedWriteHeartbeat) begin(objectID string) error {
	err := heartbeat.record(unkeyedWriteCheckpoint{
		CallID: heartbeat.call.ID, Operation: heartbeat.operation,
		Attempt: heartbeat.call.Context.Attempt(), ObjectID: objectID,
	})
	heartbeat.stopSignal = make(chan struct{})
	heartbeat.stopped = make(chan struct{})
	go heartbeat.recordUntilStopped()
	return err
}

func (heartbeat *unkeyedWriteHeartbeat) recordObjectID(objectID string) error {
	return heartbeat.record(unkeyedWriteCheckpoint{
		CallID: heartbeat.call.ID, Operation: heartbeat.operation,
		Attempt: heartbeat.call.Context.Attempt(), ObjectID: objectID,
	})
}

func (heartbeat *unkeyedWriteHeartbeat) recordCanResend() error {
	return heartbeat.record(unkeyedWriteCheckpoint{
		CallID: heartbeat.call.ID, Operation: heartbeat.operation,
		Attempt: heartbeat.call.Context.Attempt(), CanResend: true,
	})
}

func (heartbeat *unkeyedWriteHeartbeat) record(checkpoint unkeyedWriteCheckpoint) error {
	heartbeat.mutex.Lock()
	defer heartbeat.mutex.Unlock()
	heartbeat.checkpoint = checkpoint
	return heartbeat.call.Context.RecordHeartbeat(checkpoint)
}

func (heartbeat *unkeyedWriteHeartbeat) recordUntilStopped() {
	defer close(heartbeat.stopped)
	ticker := time.NewTicker(unkeyedWriteHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-heartbeat.stopSignal:
			return
		case <-heartbeat.call.Context.Done():
			return
		case <-ticker.C:
			heartbeat.mutex.Lock()
			// A lost heartbeat only risks timeout; the next attempt fails closed without its dispatch marker.
			_ = heartbeat.call.Context.RecordHeartbeat(heartbeat.checkpoint)
			heartbeat.mutex.Unlock()
		}
	}
}

func (heartbeat *unkeyedWriteHeartbeat) stop() {
	if heartbeat.stopSignal != nil {
		close(heartbeat.stopSignal)
		<-heartbeat.stopped
	}
}

func previousUnkeyedWriteCheckpoint(call sdkgo.Call, operation string) (unkeyedWriteCheckpoint, bool, error) {
	var checkpoint unkeyedWriteCheckpoint
	hasCheckpoint, err := call.Context.GetLastHeartbeatValue(&checkpoint)
	if err != nil {
		return checkpoint, false, err
	}
	if hasCheckpoint && (checkpoint.CallID != call.ID || checkpoint.Operation != operation) {
		return checkpoint, false, errForeignCheckpoint
	}
	return checkpoint, hasCheckpoint, nil
}
