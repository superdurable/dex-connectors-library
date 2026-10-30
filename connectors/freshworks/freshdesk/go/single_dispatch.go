// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk

import (
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// singleDispatchMarker is the Dex heartbeat checkpoint recorded before a non-idempotent request is sent.
type singleDispatchMarker struct {
	DispatchedCallID  sdkgo.CallID `json:"freshdeskDispatchedCallId"`
	DispatchedAttempt int32        `json:"freshdeskDispatchedAttempt,omitempty"`
	RetryAttempt      int32        `json:"freshdeskRetryAttempt,omitempty"`
}

// singleDispatchClaim is whether this attempt may send its non-idempotent Freshdesk request.
type singleDispatchClaim uint8

const (
	// singleDispatchGranted means no earlier attempt sent the request and the marker is recorded.
	singleDispatchGranted singleDispatchClaim = iota + 1
	// singleDispatchAlreadySent means an earlier attempt of this Step execution may have sent it.
	singleDispatchAlreadySent
	// singleDispatchNotRecorded means Dex did not accept the marker, so nothing may be sent yet.
	singleDispatchNotRecorded
)

// inspectSingleDispatch checks the previous marker before credentials can hide an earlier send.
func inspectSingleDispatch(call sdkgo.Call) singleDispatchClaim {
	var marker singleDispatchMarker
	isMarkerFound, err := call.Context.GetLastHeartbeatValue(&marker)
	if err != nil || (isMarkerFound && (marker.DispatchedCallID != call.ID || marker.RetryAttempt != call.Context.Attempt())) {
		return singleDispatchAlreadySent
	}
	if !isMarkerFound && call.Context.Attempt() != 1 {
		return singleDispatchAlreadySent
	}
	return singleDispatchGranted
}

// recordSingleDispatch checkpoints this attempt before its non-idempotent request.
func recordSingleDispatch(call sdkgo.Call) singleDispatchClaim {
	if err := call.Context.RecordHeartbeat(singleDispatchMarker{DispatchedCallID: call.ID, DispatchedAttempt: call.Context.Attempt()}); err != nil {
		permitSingleDispatchRetry(call)
		return singleDispatchNotRecorded
	}
	return singleDispatchGranted
}

// permitSingleDispatchRetry records which next attempt may resend after a safe retry outcome.
func permitSingleDispatchRetry(call sdkgo.Call) {
	// A failed or unacknowledged heartbeat makes the next attempt fail closed.
	_ = call.Context.RecordHeartbeat(singleDispatchMarker{DispatchedCallID: call.ID, RetryAttempt: call.Context.Attempt() + 1})
}

// singleDispatchBranches names an operation's branches for the shared single-dispatch outcome mapping.
type singleDispatchBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	defect           sdkgo.BranchID
}

// singleDispatchAttemptBeforeSend returns the attempt for a claim that does not permit sending.
func singleDispatchAttemptBeforeSend[OUT any](claim singleDispatchClaim, operation string, receipt sdkgo.Receipt) (sdkgo.MutationAttempt[OUT], bool) {
	switch claim {
	case singleDispatchGranted:
		return sdkgo.MutationAttempt[OUT]{}, false
	case singleDispatchNotRecorded:
		return sdkgo.NewMutationRetry[OUT](freshdeskFailure(sdkgo.FailureAvailability, operation, "Dex did not record the dispatch checkpoint; nothing was sent"), 0), true
	default:
		var zero OUT
		return sdkgo.NewMutationUncertain(zero, freshdeskFailure(sdkgo.FailureTransport, operation,
			"an earlier attempt of this Step may have sent the request, so it is not sent again"), receipt), true
	}
}

// singleDispatchAttemptForExchange retries 429, 503, and proven unsent requests with a next-attempt permit.
func singleDispatchAttemptForExchange[OUT any](call sdkgo.Call, result freshdeskExchange, receipt sdkgo.Receipt, branches singleDispatchBranches) (sdkgo.MutationAttempt[OUT], bool) {
	var zero OUT
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[OUT]{}, false
	case exchangeRateLimited, exchangeNotSent, exchangeServiceUnavailable:
		permitSingleDispatchRetry(call)
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	case exchangeUnavailable, exchangeInvalid:
		return sdkgo.NewMutationUncertain(zero, result.failure, receipt), true
	case exchangeNotFound:
		if branches.notFound != "" {
			return sdkgo.NewMutationBranch(branches.notFound, zero, &result.failure, receipt), true
		}
		return sdkgo.NewMutationBranch(branches.providerRejected, zero, &result.failure, receipt), true
	case exchangeDefect:
		return sdkgo.NewMutationBranch(branches.defect, zero, &result.failure, receipt), true
	default:
		return sdkgo.NewMutationBranch(branches.providerRejected, zero, &result.failure, receipt), true
	}
}
