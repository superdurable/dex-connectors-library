// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk

import (
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// singleDispatchMarker is the Dex heartbeat checkpoint recorded before a non-idempotent request is sent.
type singleDispatchMarker struct {
	DispatchedCallID sdkgo.CallID `json:"freshdeskDispatchedCallId"`
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

// claimSingleDispatch records the marker before the first send; a later attempt that finds it must not resend.
func claimSingleDispatch(call sdkgo.Call) singleDispatchClaim {
	var marker singleDispatchMarker
	isMarkerFound, err := call.Context.GetLastHeartbeatValue(&marker)
	if err != nil || isMarkerFound {
		return singleDispatchAlreadySent
	}
	if err := call.Context.RecordHeartbeat(singleDispatchMarker{DispatchedCallID: call.ID}); err != nil {
		return singleDispatchNotRecorded
	}
	return singleDispatchGranted
}

// releaseSingleDispatch clears the marker after Freshdesk provably did not apply the request.
func releaseSingleDispatch(call sdkgo.Call) {
	// A failed clear leaves the marker, so the next attempt selects uncertain instead of resending.
	_ = call.Context.RecordHeartbeat(nil)
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

// singleDispatchAttemptForExchange maps a sent request: only a provable non-application is retried.
func singleDispatchAttemptForExchange[OUT any](call sdkgo.Call, result freshdeskExchange, receipt sdkgo.Receipt, branches singleDispatchBranches) (sdkgo.MutationAttempt[OUT], bool) {
	var zero OUT
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[OUT]{}, false
	case exchangeRateLimited, exchangeNotSent:
		releaseSingleDispatch(call)
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	case exchangeUnavailable, exchangeInvalid:
		return sdkgo.NewMutationUncertain(zero, result.failure, receipt), true
	case exchangeNotFound:
		if branches.notFound != "" {
			return sdkgo.NewMutationBranch(branches.notFound, zero, &result.failure, receipt), true
		}
		return sdkgo.NewMutationBranch(branches.providerRejected, zero, &result.failure, receipt), true
	case exchangeDefect:
		releaseSingleDispatch(call)
		return sdkgo.NewMutationBranch(branches.defect, zero, &result.failure, receipt), true
	default:
		return sdkgo.NewMutationBranch(branches.providerRejected, zero, &result.failure, receipt), true
	}
}
