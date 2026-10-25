// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai

import (
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// checkpointHeartbeatInterval beats twice within Dex's 10-second minimum heartbeat timeout.
const checkpointHeartbeatInterval = 5 * time.Second

// createResponseCheckpoint is the Dex heartbeat value that fences createResponse
// against a second billed request. Dex may lose it, so a missing or stale
// checkpoint never permits a new request.
type createResponseCheckpoint struct {
	CallID  sdkgo.CallID `json:"callId"`
	Attempt int32        `json:"attempt"`
	// ResponseID makes every later attempt read the response back.
	ResponseID string `json:"responseId,omitempty"`
	// CanResend permits only the next attempt to send, after this attempt proved no response exists.
	CanResend bool `json:"canResend,omitempty"`
}

// createResponseHeartbeat is the only heartbeat writer of one attempt. It
// never records nil, because a nil heartbeat clears the checkpoint.
type createResponseHeartbeat struct {
	call       sdkgo.Call
	interval   time.Duration
	mu         sync.Mutex
	checkpoint createResponseCheckpoint
	stopSignal chan struct{}
	stopped    chan struct{}
}

func newCreateResponseHeartbeat(call sdkgo.Call, interval time.Duration) *createResponseHeartbeat {
	return &createResponseHeartbeat{call: call, interval: interval}
}

// begin records the attempt's first checkpoint, which Dex sends at once, and
// then re-records the current checkpoint on a ticker until stop.
func (heartbeat *createResponseHeartbeat) begin(checkpoint createResponseCheckpoint) error {
	err := heartbeat.record(checkpoint)
	heartbeat.stopSignal = make(chan struct{})
	heartbeat.stopped = make(chan struct{})
	go heartbeat.recordUntilStopped()
	return err
}

func (heartbeat *createResponseHeartbeat) recordResponseID(responseID string) error {
	return heartbeat.record(createResponseCheckpoint{
		CallID: heartbeat.call.ID, Attempt: heartbeat.call.Context.Attempt(), ResponseID: responseID,
	})
}

func (heartbeat *createResponseHeartbeat) recordCanResend() error {
	return heartbeat.record(createResponseCheckpoint{
		CallID: heartbeat.call.ID, Attempt: heartbeat.call.Context.Attempt(), CanResend: true,
	})
}

func (heartbeat *createResponseHeartbeat) record(checkpoint createResponseCheckpoint) error {
	heartbeat.mu.Lock()
	defer heartbeat.mu.Unlock()
	heartbeat.checkpoint = checkpoint
	return heartbeat.call.Context.RecordHeartbeat(checkpoint)
}

func (heartbeat *createResponseHeartbeat) recordUntilStopped() {
	defer close(heartbeat.stopped)
	ticker := time.NewTicker(heartbeat.interval)
	defer ticker.Stop()
	for {
		select {
		case <-heartbeat.stopSignal:
			return
		case <-heartbeat.call.Context.Done():
			return
		case <-ticker.C:
			heartbeat.mu.Lock()
			// A failed beat only risks a heartbeat timeout; the next tick records the checkpoint again.
			_ = heartbeat.call.Context.RecordHeartbeat(heartbeat.checkpoint)
			heartbeat.mu.Unlock()
		}
	}
}

// stop returns after the last heartbeat, because Dex rejects heartbeats after Execute returns.
func (heartbeat *createResponseHeartbeat) stop() {
	if heartbeat.stopSignal == nil {
		return
	}
	close(heartbeat.stopSignal)
	<-heartbeat.stopped
}
