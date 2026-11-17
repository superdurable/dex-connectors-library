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

// createResponseCheckpoint fences createResponse against a second billed request; a missing or stale one never permits it.
type createResponseCheckpoint struct {
	CallID  sdkgo.CallID `json:"callId"`
	Attempt int32        `json:"attempt"`
	// ResponseID makes every later attempt read the response back.
	ResponseID string `json:"responseId,omitempty"`
	// CanResend permits only the next attempt to send, after this attempt proved no response exists.
	CanResend bool `json:"canResend,omitempty"`
	// SendCount bounds possibly effective creates through Attempt; nil in checkpoints of earlier releases.
	SendCount *int32 `json:"sendCount,omitempty"`
}

// createResponseSendBudget bounds possibly effective creates of one call across attempts, as the
// single-dispatch Fence does, so an unknown outcome resends only below call.MaxSends().
type createResponseSendBudget struct {
	priorSendUpperBound int32
	maxSends            int32
}

// newCreateResponseSendBudget trusts a valid SendCount, adds one possible send per attempt after it,
// and otherwise falls back to attempt - 1, because each attempt sends at most once.
func newCreateResponseSendBudget(call sdkgo.Call, previous createResponseCheckpoint, isTrusted bool) createResponseSendBudget {
	attempt := call.Context.Attempt()
	bound := attempt - 1
	if isTrusted && previous.SendCount != nil && previous.Attempt >= 1 && previous.Attempt < attempt &&
		*previous.SendCount >= 0 && *previous.SendCount <= previous.Attempt {
		bound = *previous.SendCount + attempt - 1 - previous.Attempt
	}
	return createResponseSendBudget{priorSendUpperBound: bound, maxSends: call.MaxSends()}
}

// canSendNow reports whether this attempt may send without exceeding the cap.
func (budget createResponseSendBudget) canSendNow() bool {
	return budget.priorSendUpperBound < budget.maxSends
}

// canResendLater reports whether a later attempt may send after this attempt possibly sent.
func (budget createResponseSendBudget) canResendLater() bool {
	return budget.priorSendUpperBound+1 < budget.maxSends
}

// hasPossibleEarlierSend reports whether an earlier attempt may have created a response.
func (budget createResponseSendBudget) hasPossibleEarlierSend() bool {
	return budget.priorSendUpperBound > 0
}

// createResponseHeartbeat is an attempt's only heartbeat writer; it never records nil, which clears the checkpoint.
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

// begin records the attempt's first checkpoint, then re-records the current one on a ticker until stop.
func (heartbeat *createResponseHeartbeat) begin(checkpoint createResponseCheckpoint) error {
	err := heartbeat.record(checkpoint)
	heartbeat.stopSignal = make(chan struct{})
	heartbeat.stopped = make(chan struct{})
	go heartbeat.recordUntilStopped()
	return err
}

// recordResponseID keeps the claim's SendCount, since the named response is the claimed send.
func (heartbeat *createResponseHeartbeat) recordResponseID(responseID string) error {
	return heartbeat.record(createResponseCheckpoint{
		CallID: heartbeat.call.ID, Attempt: heartbeat.call.Context.Attempt(), ResponseID: responseID,
		SendCount: heartbeat.currentSendCount(),
	})
}

// recordCanResend records sendCount, the possibly effective creates through this attempt.
func (heartbeat *createResponseHeartbeat) recordCanResend(sendCount int32) error {
	return heartbeat.record(createResponseCheckpoint{
		CallID: heartbeat.call.ID, Attempt: heartbeat.call.Context.Attempt(), CanResend: true, SendCount: &sendCount,
	})
}

// currentSendCount returns the SendCount of the checkpoint this attempt last recorded.
func (heartbeat *createResponseHeartbeat) currentSendCount() *int32 {
	heartbeat.mu.Lock()
	defer heartbeat.mu.Unlock()
	return heartbeat.checkpoint.SendCount
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
