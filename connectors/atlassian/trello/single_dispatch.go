// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello

import (
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// singleDispatchMarker is the Dex heartbeat checkpoint recorded before a non-idempotent request is sent.
type singleDispatchMarker struct {
	DispatchedCallID sdkgo.CallID `json:"trelloDispatchedCallId"`
}

// singleDispatchClaim is whether this attempt may send its non-idempotent Trello request.
type singleDispatchClaim uint8

const (
	// singleDispatchGranted means no earlier attempt sent the request and the marker is recorded.
	singleDispatchGranted singleDispatchClaim = iota + 1
	// singleDispatchAlreadySent means an earlier attempt of this Step execution may have sent it.
	singleDispatchAlreadySent
	// singleDispatchNotRecorded means Dex did not accept the marker, so nothing may be sent yet.
	singleDispatchNotRecorded
)

const earlierDispatchMessage = "an earlier attempt of this Step may have sent the request, so it is not sent again"

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

// releaseSingleDispatch clears the marker after Trello provably did not apply the request.
func releaseSingleDispatch(call sdkgo.Call) {
	// A failed clear leaves the marker, so the next attempt selects uncertain instead of resending.
	_ = call.Context.RecordHeartbeat(nil)
}

// singleDispatchAttemptBeforeSend ends the attempt unless the claim was granted.
func singleDispatchAttemptBeforeSend[OUT any](claim singleDispatchClaim, operationID string, requested OUT, receipt sdkgo.Receipt) (sdkgo.MutationAttempt[OUT], bool) {
	switch claim {
	case singleDispatchGranted:
		return sdkgo.MutationAttempt[OUT]{}, false
	case singleDispatchNotRecorded:
		return sdkgo.NewMutationRetry[OUT](newFailure(operationID, sdkgo.FailureAvailability, "Dex did not record the dispatch checkpoint, so nothing was sent to Trello"), 0), true
	default:
		return sdkgo.NewMutationUncertain(requested, newFailure(operationID, sdkgo.FailureTransport, earlierDispatchMessage), receipt), true
	}
}

// singleDispatchBranches names an operation's branches for the shared single-dispatch outcome mapping.
type singleDispatchBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	defect           sdkgo.BranchID
}

// singleDispatchAttemptForWrite maps a send; only a provable non-application releases the marker and retries.
func singleDispatchAttemptForWrite[OUT any](call sdkgo.Call, classification writeClassification, requested OUT, receipt sdkgo.Receipt, branches singleDispatchBranches) (sdkgo.MutationAttempt[OUT], bool) {
	switch classification.outcome {
	case writeAccepted:
		return sdkgo.MutationAttempt[OUT]{}, false
	case writeRetry:
		releaseSingleDispatch(call)
		return sdkgo.NewMutationRetry[OUT](classification.failure, classification.retryAfter), true
	case writeDefect:
		releaseSingleDispatch(call)
		return sdkgo.NewMutationBranch(branches.defect, requested, &classification.failure, receipt), true
	case writeNotFound:
		if branches.notFound != "" {
			return sdkgo.NewMutationBranch(branches.notFound, requested, &classification.failure, receipt), true
		}
		return sdkgo.NewMutationBranch(branches.providerRejected, requested, &classification.failure, receipt), true
	case writeRejected:
		return sdkgo.NewMutationBranch(branches.providerRejected, requested, &classification.failure, receipt), true
	default:
		return sdkgo.NewMutationUncertain(requested, classification.failure, receipt), true
	}
}
