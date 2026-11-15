// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package singledispatch fences one non-idempotent provider write per Dex Step call.
//
// By default a Fence never resends after a possible dispatch. WithMaxSends enables bounded
// resend for a repeatOnUnknown: limited operation: a later attempt may read back and then
// send again while the possibly effective sends of the Call ID stay below the cap.
package singledispatch

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/fenceobservation"
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
	// SendCount is the number of possibly effective sends this Call ID made up to and
	// including Attempt. A claimed marker includes its own send; notSent and canResend
	// markers carry the count from before their attempt's claim. Only a Fence with a
	// WithMaxSends cap of 2 or more writes it, so other markers stay as before sdkgo
	// v0.37.0. JSON omits zero, and Decide reads an absent count as Attempt for claimed
	// and Attempt-1 otherwise, which can only overcount.
	SendCount int32 `json:"sendCount,omitempty"`
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
	// ReadBackThenResendAllowed means an earlier attempt may have sent, but fewer sends
	// than the WithMaxSends cap may have taken effect. The connector reads back first when
	// it can: a confirmed write selects the happy path, and a definite absence or an
	// inconclusive read-back permits one Dispatch. A connector without read-back may call
	// Dispatch directly. A saved Marker.ProviderID must be read back before any resend.
	// A pre-send Retry records nothing, so the next attempt reads back again.
	// Only a Fence constructed with WithMaxSends of 2 or more returns it.
	ReadBackThenResendAllowed
)

// Decision contains a same-call prior marker, when present, and a heartbeat read error.
// A caller must resolve credentials only after inspecting this decision.
type Decision struct {
	// Kind identifies the allowed action.
	Kind DecisionKind
	// Marker is the prior checkpoint, including any provider identity.
	Marker Marker
	// Err reports an unreadable or invalid checkpoint.
	Err error
	// MaximumPriorSends is an upper bound on the possibly effective sends of this Call ID
	// before the current attempt. It is zero for FailClosed.
	MaximumPriorSends int32
}

var (
	// ErrDispatchNotAllowed means this attempt has no permission to send.
	ErrDispatchNotAllowed = errors.New("single dispatch is not allowed for this attempt")
	// ErrInsufficientTime means the request cannot fit in the attempt budget.
	ErrInsufficientTime = errors.New("insufficient attempt time for single dispatch")
	// ErrCheckpoint means the claim heartbeat failed before the request was sent.
	ErrCheckpoint = errors.New("single-dispatch checkpoint could not be recorded")
)

// Fence owns the checkpoint and send decision for one Step call and attempt.
// Construct exactly one per call per attempt. Its methods are safe for concurrent callers.
// A five-second ticker repeats the claim while Dispatch runs; a slow heartbeat can delay Dispatch's return.
type Fence struct {
	call               sdkgo.Call
	writeDeadline      time.Time
	minimumRequestTime time.Duration
	heartbeatInterval  time.Duration
	maxSends           int32
	mu                 sync.Mutex
	decision           Decision
	hasDecision        bool
	hasDispatched      bool
	hasSendFinished    bool
	hasPreSendRetry    bool
	marker             Marker
	claimSendCount     int32
}

// FenceOption configures the write window or send cap of a Fence.
type FenceOption interface{ applyFence(*fenceConfig) }

type fenceConfig struct {
	writeWindowFraction float64
	maxSends            int32
}

type writeWindowOption struct{ writeWindowFraction float64 }

func (option writeWindowOption) applyFence(config *fenceConfig) {
	config.writeWindowFraction = option.writeWindowFraction
}

type maxSendsOption struct{ maxSends int32 }

func (option maxSendsOption) applyFence(config *fenceConfig) {
	config.maxSends = option.maxSends
}

// WithWriteWindow sets the fraction of the initial attempt budget available to the request.
// Use 7/12 when read-back needs time; at most 5/6 is allowed when no read-back is needed.
// NewFence rejects values outside (0, 5/6], including NaN and infinity.
func WithWriteWindow(fraction float64) FenceOption {
	return writeWindowOption{writeWindowFraction: fraction}
}

// WithMaxSends caps the possibly effective sends of the call's Call ID across all attempts.
// Pass sdkgo.Call.MaxSends, which carries the manifest's execution.maxSends or the
// application's per-Step override; never hard-code the cap. One, the default without this
// option, never resends after a possible dispatch. From 2, Decide returns
// ReadBackThenResendAllowed while fewer than maxSends sends may have taken effect.
// NewFence rejects values outside 1 through sdkgo.MaxSendsCeiling.
func WithMaxSends(maxSends int32) FenceOption {
	return maxSendsOption{maxSends: maxSends}
}

// NewFence captures the smaller of Execute timeout and the current Step deadline.
// The default request context ends after 7/12 of that budget; WithWriteWindow can use up to 5/6.
// Dispatch refuses a send when less than one tenth of the initial budget remains before that deadline.
// executeTimeout must be positive; the operation must use sync Execute durability.
func NewFence(call sdkgo.Call, executeTimeout time.Duration, options ...FenceOption) (*Fence, error) {
	if call.Context == nil || call.ID == "" || call.Context.Attempt() < 1 || executeTimeout <= 0 {
		return nil, errors.New("single dispatch requires a call, positive attempt, and Execute timeout")
	}
	config := fenceConfig{writeWindowFraction: 7.0 / 12.0, maxSends: 1}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("single-dispatch FenceOption is nil")
		}
		option.applyFence(&config)
	}
	if math.IsNaN(config.writeWindowFraction) || math.IsInf(config.writeWindowFraction, 0) ||
		config.writeWindowFraction <= 0 || config.writeWindowFraction > 5.0/6.0 {
		return nil, errors.New("single-dispatch write window must be within (0, 5/6]")
	}
	if config.maxSends < 1 || config.maxSends > sdkgo.MaxSendsCeiling {
		return nil, fmt.Errorf("single-dispatch max sends must be within 1 through %d", sdkgo.MaxSendsCeiling)
	}
	startedAt := time.Now()
	budget := executeTimeout
	if deadline, hasDeadline := call.Context.Deadline(); hasDeadline {
		budget = min(budget, deadline.Sub(startedAt))
	}
	budget = max(budget, 0)
	return &Fence{
		call:               call,
		writeDeadline:      startedAt.Add(time.Duration(float64(budget) * config.writeWindowFraction)),
		minimumRequestTime: budget / 10, heartbeatInterval: 5 * time.Second,
		maxSends: config.maxSends,
	}, nil
}

// Decide reads the last heartbeat before any credential or provider lookup.
// On a later attempt, a missing, foreign, stale, claimed, or invalid checkpoint forbids a send,
// except that a WithMaxSends cap of 2 or more permits a read-back and resend while the
// upper bound of earlier sends stays below the cap. Foreign and invalid checkpoints always fail closed.
func (fence *Fence) Decide() Decision {
	fence.mu.Lock()
	defer fence.mu.Unlock()
	if fence.hasDecision {
		return fence.decision
	}
	fence.hasDecision = true
	defer func() {
		fenceobservation.ReportDecision(fence.attemptIdentity(), fence.decision.Kind == SendAllowed)
	}()
	attempt := fence.call.Context.Attempt()
	var previous Marker
	hasPrevious, err := fence.call.Context.GetLastHeartbeatValue(&previous)
	if err != nil {
		fence.decision = Decision{Kind: FailClosed, Err: err}
		return fence.decision
	}
	if !hasPrevious {
		if attempt == 1 {
			fence.decision = Decision{Kind: SendAllowed}
		} else {
			// Each earlier attempt sends at most once.
			fence.decision = Decision{Kind: fence.resendOrReadBack(attempt - 1), MaximumPriorSends: attempt - 1}
		}
		return fence.decision
	}
	if previous.CallID != fence.call.ID {
		fence.decision = Decision{Kind: FailClosed, Err: errors.New("foreign single-dispatch checkpoint")}
		return fence.decision
	}
	if !isValidMarker(previous) {
		fence.decision = Decision{Kind: FailClosed, Marker: previous, Err: errors.New("invalid single-dispatch checkpoint")}
		return fence.decision
	}
	priorSends := maximumPriorSends(previous, attempt)
	fence.decision = Decision{Kind: ReadBackRequired, Marker: previous, MaximumPriorSends: priorSends}
	if previous.Attempt == attempt-1 &&
		(previous.State == StateNotSent || previous.State == StateCanResend) && previous.ProviderID == "" {
		fence.decision.Kind = SendAllowed
		return fence.decision
	}
	// A marker from this or a later attempt is anomalous, so it never permits a resend.
	if previous.Attempt < attempt {
		fence.decision.Kind = fence.resendOrReadBack(priorSends)
	}
	return fence.decision
}

// Dispatch records the claim as the send path's first heartbeat, then calls send once.
// It requires a SendAllowed or ReadBackThenResendAllowed decision; the claim's SendCount
// is the decision's MaximumPriorSends plus one.
// A heartbeat failure or short budget returns an error without calling send; return Retry.
// Only after SendAllowed does that path also record notSent.
// The request context ends at the configured write window, leaving time to classify its outcome.
func (fence *Fence) Dispatch(send func(context.Context) error) error {
	fence.mu.Lock()
	if !fence.hasDecision || !isSendDecision(fence.decision.Kind) || fence.hasDispatched || fence.hasPreSendRetry || send == nil {
		fence.mu.Unlock()
		return ErrDispatchNotAllowed
	}
	if fence.minimumRequestTime <= 0 || time.Until(fence.writeDeadline) < fence.minimumRequestTime {
		if fence.decision.Kind != SendAllowed {
			fence.hasPreSendRetry = true
			fence.mu.Unlock()
			return ErrInsufficientTime
		}
		fence.mu.Unlock()
		if err := fence.RecordNotSent(); err != nil {
			return errors.Join(ErrInsufficientTime, err)
		}
		return ErrInsufficientTime
	}
	fence.hasDispatched = true
	fence.claimSendCount = fence.decision.MaximumPriorSends + 1
	claim := Marker{CallID: fence.call.ID, Attempt: fence.call.Context.Attempt(), State: StateClaimed, SendCount: fence.markerSendCount(fence.claimSendCount)}
	if err := fence.record(claim); err != nil {
		fence.hasPreSendRetry = true
		// A failed claim never permits a send; a best-effort notSent may permit the next attempt.
		// After a possible earlier send, notSent would let the next attempt skip read-back.
		if fence.decision.Kind == SendAllowed {
			_ = fence.record(Marker{CallID: fence.call.ID, Attempt: fence.call.Context.Attempt(), State: StateNotSent, SendCount: fence.markerSendCount(fence.decision.MaximumPriorSends)})
		}
		fence.mu.Unlock()
		return errors.Join(ErrCheckpoint, err)
	}
	fence.mu.Unlock()

	requestContext, cancel := context.WithDeadline(fence.call.Context, fence.writeDeadline)
	defer cancel()
	stop := make(chan struct{})
	stopped := make(chan struct{})
	go fence.repeatClaim(claim, stop, stopped)
	defer func() {
		close(stop)
		<-stopped
	}()
	err := send(requestContext)
	fence.mu.Lock()
	fence.hasSendFinished = true
	fence.mu.Unlock()
	return err
}

// RecordNotSent checkpoints a decided pre-send Retry, including credential failure.
// It requires a SendAllowed decision and permanently forbids Dispatch in this attempt,
// even when its heartbeat write fails. Under ReadBackThenResendAllowed an earlier send may
// have taken effect, so a pre-send Retry records nothing and the next attempt reads back again.
func (fence *Fence) RecordNotSent() error {
	fence.mu.Lock()
	defer fence.mu.Unlock()
	if !fence.hasDecision || fence.decision.Kind != SendAllowed || fence.hasDispatched || fence.hasPreSendRetry {
		return ErrDispatchNotAllowed
	}
	fence.hasPreSendRetry = true
	return fence.record(Marker{CallID: fence.call.ID, Attempt: fence.call.Context.Attempt(), State: StateNotSent, SendCount: fence.markerSendCount(fence.decision.MaximumPriorSends)})
}

// RecordCanResend checkpoints positive evidence that Dispatch was not processed.
// Call it before returning Retry for a 429, 503, or proven undispatched transport failure.
func (fence *Fence) RecordCanResend() error {
	fence.mu.Lock()
	defer fence.mu.Unlock()
	if !fence.hasSendFinished || fence.marker.State != StateClaimed || fence.marker.ProviderID != "" {
		return ErrDispatchNotAllowed
	}
	// The provider did not process this attempt's send, so the count before its claim stands.
	return fence.record(Marker{CallID: fence.call.ID, Attempt: fence.call.Context.Attempt(), State: StateCanResend, SendCount: fence.markerSendCount(fence.claimSendCount - 1)})
}

// RecordProviderID checkpoints the accepted resource identity for later read-back.
// An empty provider ID is rejected; a lost heartbeat still never grants resend.
func (fence *Fence) RecordProviderID(providerID string) error {
	fence.mu.Lock()
	defer fence.mu.Unlock()
	if !fence.hasSendFinished || fence.marker.State != StateClaimed || providerID == "" {
		return ErrDispatchNotAllowed
	}
	return fence.record(Marker{CallID: fence.call.ID, Attempt: fence.call.Context.Attempt(), State: StateClaimed, ProviderID: providerID, SendCount: fence.markerSendCount(fence.claimSendCount)})
}

// markerSendCount keeps markers of a Fence without bounded resend identical to earlier releases.
func (fence *Fence) markerSendCount(sendCount int32) int32 {
	if fence.maxSends < 2 {
		return 0
	}
	return sendCount
}

// resendOrReadBack applies the send cap to a decision that would otherwise require read-back.
func (fence *Fence) resendOrReadBack(priorSends int32) DecisionKind {
	if fence.maxSends >= 2 && priorSends < fence.maxSends {
		return ReadBackThenResendAllowed
	}
	return ReadBackRequired
}

func (fence *Fence) record(marker Marker) error {
	if err := fence.call.Context.RecordHeartbeat(marker); err != nil {
		return err
	}
	fence.marker = marker
	if marker.State == StateClaimed {
		fenceobservation.ReportClaimed(fence.attemptIdentity())
	}
	return nil
}

func (fence *Fence) attemptIdentity() fenceobservation.AttemptIdentity {
	return fenceobservation.AttemptIdentity{CallID: string(fence.call.ID), Attempt: fence.call.Context.Attempt()}
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
				// The claim is best-effort repetition; failure cannot authorize another send.
				_ = fence.call.Context.RecordHeartbeat(claim)
			}
			fence.mu.Unlock()
		}
	}
}

func isSendDecision(kind DecisionKind) bool {
	return kind == SendAllowed || kind == ReadBackThenResendAllowed
}

// isValidMarker rejects a send count that one send per attempt could not produce.
func isValidMarker(marker Marker) bool {
	if marker.CallID == "" || marker.Attempt < 1 || marker.SendCount < 0 {
		return false
	}
	switch marker.State {
	case StateClaimed:
		return marker.SendCount <= marker.Attempt
	case StateNotSent, StateCanResend:
		return marker.SendCount <= marker.Attempt-1
	default:
		return false
	}
}

// maximumPriorSends bounds the sends before attempt from a valid same-call marker.
func maximumPriorSends(marker Marker, attempt int32) int32 {
	sendCount := marker.SendCount
	if sendCount == 0 {
		sendCount = marker.Attempt
		if marker.State != StateClaimed {
			sendCount--
		}
	}
	return sendCount + max(attempt-1-marker.Attempt, 0)
}
