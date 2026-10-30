// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp

import "github.com/superdurable/dex-connectors-library/sdkgo"

// sendDispatchMarker is the Dex heartbeat checkpoint recorded just before a campaign send is dispatched.
type sendDispatchMarker struct {
	DispatchedCallID sdkgo.CallID `json:"mailchimpDispatchedCallId"`
}

// hasEarlierDispatch reports a possible earlier send in this Step execution; an unreadable checkpoint counts.
func hasEarlierDispatch(call sdkgo.Call) bool {
	var marker sendDispatchMarker
	isMarkerFound, err := call.Context.GetLastHeartbeatValue(&marker)
	return err != nil || isMarkerFound
}

// recordDispatch stores the checkpoint; the send may be dispatched only after Dex accepted it.
func recordDispatch(call sdkgo.Call) error {
	return call.Context.RecordHeartbeat(sendDispatchMarker{DispatchedCallID: call.ID})
}

// releaseDispatch clears the checkpoint after Mailchimp provably did not apply the send.
func releaseDispatch(call sdkgo.Call) {
	// A failed clear leaves the checkpoint, so the next attempt reports uncertain instead of resending.
	_ = call.Context.RecordHeartbeat(nil)
}
