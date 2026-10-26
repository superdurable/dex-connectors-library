// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira

import (
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// singleDispatchMarker is the Dex heartbeat checkpoint recorded before a non-idempotent request is sent.
type singleDispatchMarker struct {
	DispatchedCallID sdkgo.CallID `json:"jiraDispatchedCallId"`
	IsRetrySafe      bool         `json:"jiraRetrySafe"`
}

// singleDispatchClaim is whether this attempt may send its non-idempotent request.
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
	if err != nil {
		return singleDispatchAlreadySent
	}
	if isMarkerFound {
		if marker.DispatchedCallID != call.ID || !marker.IsRetrySafe {
			return singleDispatchAlreadySent
		}
	} else if call.Context.Attempt() > 1 {
		return singleDispatchAlreadySent
	}
	if err := call.Context.RecordHeartbeat(singleDispatchMarker{DispatchedCallID: call.ID}); err != nil {
		return singleDispatchNotRecorded
	}
	return singleDispatchGranted
}

// releaseSingleDispatch records a safe retry after the provider provably did not apply the request.
func releaseSingleDispatch(call sdkgo.Call) {
	// A failed update leaves the dispatch marker, so the next attempt cannot resend.
	_ = call.Context.RecordHeartbeat(singleDispatchMarker{DispatchedCallID: call.ID, IsRetrySafe: true})
}

// singleDispatchAttemptBeforeSend returns the attempt for a claim that does not permit sending.
func singleDispatchAttemptBeforeSend[OUT any](claim singleDispatchClaim, operationID string, echo OUT, receipt sdkgo.Receipt) (sdkgo.MutationAttempt[OUT], bool) {
	switch claim {
	case singleDispatchGranted:
		return sdkgo.MutationAttempt[OUT]{}, false
	case singleDispatchNotRecorded:
		return sdkgo.NewMutationRetry[OUT](newFailure(operationID, sdkgo.FailureAvailability, "Dex did not record the dispatch checkpoint; nothing was sent"), 0), true
	default:
		return sdkgo.NewMutationUncertain(echo, newFailure(operationID, sdkgo.FailureTransport,
			"an earlier attempt of this Step may have sent the request, so it is not sent again"), receipt), true
	}
}
