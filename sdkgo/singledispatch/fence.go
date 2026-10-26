// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package singledispatch fences one non-idempotent provider write per Dex Step call.
package singledispatch

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// State describes the last known dispatch state of one attempt.
type State string

const (
	// StateNotSent proves this attempt did not reach the provider.
	StateNotSent State = "notSent"
	// StateClaimed means the request may have reached the provider.
	StateClaimed State = "claimed"
	// StateCanResend proves a dispatched request was not processed.
	StateCanResend State = "canResend"
)

// Marker is the non-secret heartbeat checkpoint for a single call and attempt.
type Marker struct {
	// CallID identifies the stable connector call.
	CallID sdkgo.CallID `json:"callId"`
	// Attempt is the one-based Dex Execute attempt.
	Attempt int32 `json:"attempt"`
	// State records whether another attempt may send.
	State State `json:"state"`
	// ProviderID identifies an accepted write for read-back when available.
	ProviderID string `json:"providerId,omitempty"`
}

// DecisionKind describes the action permitted before credentials or provider lookups.
type DecisionKind uint8

const (
	// SendAllowed permits this attempt to call Dispatch once.
	SendAllowed DecisionKind = iota + 1
	// ReadBackRequired forbids dispatch and permits provider reconciliation.
	ReadBackRequired
	// FailClosed forbids dispatch because the heartbeat could not be trusted.
	FailClosed
)

// Decision contains the previous marker, when present, and a heartbeat read error.
// A caller must resolve credentials only after inspecting this decision.
type Decision struct {
	// Kind identifies the allowed action.
	Kind DecisionKind
	// Marker is the prior checkpoint, including any provider identity.
	Marker Marker
	// Err reports an unreadable or invalid checkpoint.
	Err error
}

var (
	// ErrDispatchNotAllowed means this attempt has no permission to send.
	ErrDispatchNotAllowed = errors.New("single dispatch is not allowed for this attempt")
	// ErrInsufficientTime means the request cannot fit in the attempt budget.
	ErrInsufficientTime = errors.New("insufficient attempt time for single dispatch")
	// ErrCheckpoint means the claim heartbeat failed before the request was sent.
	ErrCheckpoint = errors.New("single-dispatch checkpoint could not be recorded")
)

// Fence owns the checkpoint and send decision for one Step attempt.
// Construct it at Invoke entry, call Decide before provider work, and use Dispatch once.
type Fence struct {
	call               sdkgo.Call
	writeDeadline      time.Time
	minimumRequestTime time.Duration
	heartbeatInterval  time.Duration
	mu                 sync.Mutex
	decision           Decision
	decided            bool
	dispatched         bool
	sendFinished       bool
	preSendRetry       bool
	marker             Marker
}

// NewFence captures the smaller of Execute timeout and the current Step deadline.
// executeTimeout must be positive; the operation must use sync Execute durability.
func NewFence(call sdkgo.Call, executeTimeout time.Duration) (*Fence, error) {
	if call.Context == nil || call.ID == "" || call.Context.Attempt() < 1 || executeTimeout <= 0 {
		return nil, errors.New("single dispatch requires a call, positive attempt, and Execute timeout")
	}
	startedAt := time.Now()
	budget := executeTimeout
	if deadline, hasDeadline := call.Context.Deadline(); hasDeadline {
		budget = min(budget, deadline.Sub(startedAt))
	}
	budget = max(budget, 0)
	return &Fence{
		call:               call,
		writeDeadline:      startedAt.Add(budget * 7 / 12),
		minimumRequestTime: budget / 10, heartbeatInterval: 5 * time.Second,
	}, nil
}

// Decide reads the last heartbeat before any credential or provider lookup.
// A missing, foreign, stale, claimed, or invalid checkpoint never permits a send.
func (fence *Fence) Decide() Decision {
	fence.mu.Lock()
	defer fence.mu.Unlock()
	if fence.decided {
		return fence.decision
	}
	fence.decided = true
	var previous Marker
	hasPrevious, err := fence.call.Context.GetLastHeartbeatValue(&previous)
	if err != nil {
		fence.decision = Decision{Kind: FailClosed, Err: err}
		return fence.decision
	}
	if !hasPrevious {
		if fence.call.Context.Attempt() == 1 {
			fence.decision = Decision{Kind: SendAllowed}
		} else {
			fence.decision = Decision{Kind: ReadBackRequired}
		}
		return fence.decision
	}
	fence.decision = Decision{Kind: ReadBackRequired, Marker: previous}
	if previous.CallID == "" || previous.Attempt < 1 ||
		(previous.State != StateNotSent && previous.State != StateClaimed && previous.State != StateCanResend) {
		fence.decision.Kind = FailClosed
		fence.decision.Err = errors.New("invalid single-dispatch checkpoint")
		return fence.decision
	}
	if previous.CallID == fence.call.ID && previous.Attempt == fence.call.Context.Attempt()-1 &&
		(previous.State == StateNotSent || previous.State == StateCanResend) && previous.ProviderID == "" {
		fence.decision.Kind = SendAllowed
	}
	return fence.decision
}

// RecordNotSent checkpoints a decided pre-send Retry, including credential failure.
// It is refused after Dispatch starts, because the request may have reached the provider.
func (fence *Fence) RecordNotSent() error {
	fence.mu.Lock()
	defer fence.mu.Unlock()
	if !fence.decided || fence.decision.Kind != SendAllowed || fence.dispatched || fence.preSendRetry {
		return ErrDispatchNotAllowed
	}
	if err := fence.record(Marker{CallID: fence.call.ID, Attempt: fence.call.Context.Attempt(), State: StateNotSent}); err != nil {
		return err
	}
	fence.preSendRetry = true
	return nil
}

// Dispatch records the claim as the send path's first heartbeat, then calls send once.
// A heartbeat failure or short budget returns an error without calling send; return Retry.
// The request context ends before the Step attempt, leaving time to classify its outcome.
func (fence *Fence) Dispatch(send func(context.Context) error) error {
	fence.mu.Lock()
	if !fence.decided || fence.decision.Kind != SendAllowed || fence.dispatched || fence.preSendRetry || send == nil {
		fence.mu.Unlock()
		return ErrDispatchNotAllowed
	}
	if fence.minimumRequestTime <= 0 || time.Until(fence.writeDeadline) < fence.minimumRequestTime {
		fence.mu.Unlock()
		if err := fence.RecordNotSent(); err != nil {
			return errors.Join(ErrInsufficientTime, err)
		}
		return ErrInsufficientTime
	}
	fence.dispatched = true
	claim := Marker{CallID: fence.call.ID, Attempt: fence.call.Context.Attempt(), State: StateClaimed}
	if err := fence.record(claim); err != nil {
		fence.dispatched = false
		_ = fence.record(Marker{CallID: fence.call.ID, Attempt: fence.call.Context.Attempt(), State: StateNotSent})
		fence.preSendRetry = true
		fence.mu.Unlock()
		return errors.Join(ErrCheckpoint, err)
	}
	fence.mu.Unlock()

	requestContext, cancel := context.WithDeadline(fence.call.Context, fence.writeDeadline)
	defer cancel()
	stop := make(chan struct{})
	stopped := make(chan struct{})
	go fence.repeatClaim(claim, stop, stopped)
	err := send(requestContext)
	close(stop)
	<-stopped
	fence.mu.Lock()
	fence.sendFinished = true
	fence.mu.Unlock()
	return err
}

// RecordCanResend checkpoints positive evidence that Dispatch was not processed.
// Call it before returning Retry for a 429, 503, or proven undispatched transport failure.
func (fence *Fence) RecordCanResend() error {
	fence.mu.Lock()
	defer fence.mu.Unlock()
	if !fence.sendFinished || fence.marker.State != StateClaimed || fence.marker.ProviderID != "" {
		return ErrDispatchNotAllowed
	}
	return fence.record(Marker{CallID: fence.call.ID, Attempt: fence.call.Context.Attempt(), State: StateCanResend})
}

// RecordProviderID checkpoints the accepted resource identity for later read-back.
// An empty provider ID is rejected; a lost heartbeat still never grants resend.
func (fence *Fence) RecordProviderID(providerID string) error {
	fence.mu.Lock()
	defer fence.mu.Unlock()
	if !fence.sendFinished || fence.marker.State != StateClaimed || providerID == "" {
		return ErrDispatchNotAllowed
	}
	return fence.record(Marker{CallID: fence.call.ID, Attempt: fence.call.Context.Attempt(), State: StateClaimed, ProviderID: providerID})
}

func (fence *Fence) record(marker Marker) error {
	if err := fence.call.Context.RecordHeartbeat(marker); err != nil {
		return err
	}
	fence.marker = marker
	return nil
}

func (fence *Fence) repeatClaim(claim Marker, stop <-chan struct{}, stopped chan<- struct{}) {
	defer close(stopped)
	ticker := time.NewTicker(fence.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-fence.call.Context.Done():
			return
		case <-ticker.C:
			fence.mu.Lock()
			if fence.marker == claim {
				_ = fence.call.Context.RecordHeartbeat(claim)
			}
			fence.mu.Unlock()
		}
	}
}
