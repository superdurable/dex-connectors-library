// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr

import (
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// singleDispatchMarker is the Dex heartbeat checkpoint recorded before a non-idempotent request is sent.
type singleDispatchMarker struct {
	DispatchedCallID sdkgo.CallID `json:"bamboohrDispatchedCallId"`
}

// singleDispatchClaim is whether this attempt may send its non-idempotent BambooHR request.
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

// releaseSingleDispatch clears the marker after BambooHR provably did not apply the request.
func releaseSingleDispatch(call sdkgo.Call) {
	// A failed clear leaves the marker, so the next attempt selects uncertain instead of resending.
	_ = call.Context.RecordHeartbeat(nil)
}
