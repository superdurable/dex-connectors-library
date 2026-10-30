// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk

import (
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// singleDispatchMarker is the Dex heartbeat checkpoint recorded before a non-idempotent request is sent.
type singleDispatchMarker struct {
	DispatchedCallID sdkgo.CallID `json:"zohoDeskDispatchedCallId"`
}

// singleDispatchAttemptForEarlierDispatch selects uncertain when an earlier attempt of this Step
// execution recorded the marker, so it may have sent the request. Operations check it before
// resolving credentials, so a later credential failure cannot report a sent write as rejected.
func singleDispatchAttemptForEarlierDispatch[OUT any](call sdkgo.Call, operation string, receipt sdkgo.Receipt) (sdkgo.MutationAttempt[OUT], bool) {
	var marker singleDispatchMarker
	isMarkerFound, err := call.Context.GetLastHeartbeatValue(&marker)
	if err == nil && !isMarkerFound {
		return sdkgo.MutationAttempt[OUT]{}, false
	}
	var zero OUT
	return sdkgo.NewMutationUncertain(zero, deskFailure(sdkgo.FailureTransport, operation,
		"an earlier attempt of this Step may have sent the request, so it is not sent again"), receipt), true
}

// singleDispatchAttemptForRecord records the marker before the first send; nothing is sent when Dex does not accept it.
func singleDispatchAttemptForRecord[OUT any](call sdkgo.Call, operation string) (sdkgo.MutationAttempt[OUT], bool) {
	if err := call.Context.RecordHeartbeat(singleDispatchMarker{DispatchedCallID: call.ID}); err != nil {
		return sdkgo.NewMutationRetry[OUT](deskFailure(sdkgo.FailureAvailability, operation, "Dex did not record the dispatch checkpoint; nothing was sent"), 0), true
	}
	return sdkgo.MutationAttempt[OUT]{}, false
}

// releaseSingleDispatch clears the marker after Zoho Desk provably did not apply the request.
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

// singleDispatchAttemptForSession maps a session that could not start; nothing was claimed or sent.
func singleDispatchAttemptForSession[OUT any](failure *sessionFailure, branches singleDispatchBranches) sdkgo.MutationAttempt[OUT] {
	var zero OUT
	switch failure.route {
	case sessionRetry:
		return sdkgo.NewMutationRetry[OUT](failure.failure, 0)
	case sessionRejected:
		return sdkgo.NewMutationBranch(branches.providerRejected, zero, &failure.failure, sdkgo.Receipt{})
	default:
		return sdkgo.NewMutationBranch(branches.defect, zero, &failure.failure, sdkgo.Receipt{})
	}
}

// singleDispatchAttemptForExchange maps a sent request: only a provable non-application is retried.
func singleDispatchAttemptForExchange[OUT any](call sdkgo.Call, result deskExchange, receipt sdkgo.Receipt, branches singleDispatchBranches) (sdkgo.MutationAttempt[OUT], bool) {
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
