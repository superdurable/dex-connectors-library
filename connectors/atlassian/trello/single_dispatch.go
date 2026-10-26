// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello

import (
	"context"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const minimumSingleDispatchWindow = 3 * time.Second

// singleDispatchMarker is the Dex heartbeat checkpoint recorded before a non-idempotent request is sent.
type singleDispatchMarker struct {
	DispatchedCallID sdkgo.CallID `json:"trelloDispatchedCallId"`
	Attempt          int32        `json:"trelloAttempt"`
	CanRetry         bool         `json:"trelloCanRetry,omitempty"`
}

// singleDispatchClaim is whether this attempt may send its non-idempotent Trello request.
type singleDispatchClaim uint8

const (
	// singleDispatchGranted means this attempt may send after recording its claim.
	singleDispatchGranted singleDispatchClaim = iota + 1
	// singleDispatchAlreadySent means an earlier attempt of this Step execution may have sent it.
	singleDispatchAlreadySent
	// singleDispatchNotRecorded means Dex did not accept the marker, so nothing may be sent yet.
	singleDispatchNotRecorded
	// singleDispatchInsufficientTime means this attempt cannot safely start a write.
	singleDispatchInsufficientTime
)

const earlierDispatchMessage = "an earlier attempt of this Step may have sent the request, so it is not sent again"

// claimSingleDispatch permits a send only after this attempt records its own marker.
func claimSingleDispatch(call sdkgo.Call, requestContext context.Context) singleDispatchClaim {
	var marker singleDispatchMarker
	isMarkerFound, err := call.Context.GetLastHeartbeatValue(&marker)
	if err != nil {
		return singleDispatchAlreadySent
	}
	attempt := call.Context.Attempt()
	if isMarkerFound && (!marker.CanRetry || marker.DispatchedCallID != call.ID || marker.Attempt != attempt-1) {
		return singleDispatchAlreadySent
	}
	if !isMarkerFound && attempt > 1 {
		return singleDispatchAlreadySent
	}
	if deadline, hasDeadline := requestContext.Deadline(); hasDeadline && time.Until(deadline) < minimumSingleDispatchWindow {
		releaseSingleDispatch(call)
		return singleDispatchInsufficientTime
	}
	if err := call.Context.RecordHeartbeat(singleDispatchMarker{DispatchedCallID: call.ID, Attempt: attempt}); err != nil {
		return singleDispatchNotRecorded
	}
	return singleDispatchGranted
}

// releaseSingleDispatch records a safe retry; a lost checkpoint cannot authorize another send.
func releaseSingleDispatch(call sdkgo.Call) {
	// A failed release cannot authorize another attempt to send.
	_ = call.Context.RecordHeartbeat(singleDispatchMarker{DispatchedCallID: call.ID, Attempt: call.Context.Attempt(), CanRetry: true})
}

// singleDispatchAttemptBeforeSend ends the attempt unless the claim was granted.
func singleDispatchAttemptBeforeSend[OUT any](call sdkgo.Call, claim singleDispatchClaim, operationID string, requested OUT, receipt sdkgo.Receipt) (sdkgo.MutationAttempt[OUT], bool) {
	switch claim {
	case singleDispatchGranted:
		return sdkgo.MutationAttempt[OUT]{}, false
	case singleDispatchNotRecorded:
		releaseSingleDispatch(call)
		return sdkgo.NewMutationRetry[OUT](newFailure(operationID, sdkgo.FailureAvailability, "Dex did not record the dispatch checkpoint, so nothing was sent to Trello"), 0), true
	case singleDispatchInsufficientTime:
		return sdkgo.NewMutationRetry[OUT](newFailure(operationID, sdkgo.FailureAvailability, "The attempt has too little time left to send a Trello write"), 0), true
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

// singleDispatchAttemptForWrite maps a send; safe retries and local defects release the marker.
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
