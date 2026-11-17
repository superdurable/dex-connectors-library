// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp

import "github.com/superdurable/dex-connectors-library/sdkgo"

// legacySendDispatchMarker is the checkpoint connector releases before v0.24.1 recorded before a send.
type legacySendDispatchMarker struct {
	DispatchedCallID sdkgo.CallID `json:"mailchimpDispatchedCallId"`
}

// hasLegacyDispatch reports a pre-fence checkpoint of this call; such a Step execution never sends again.
func hasLegacyDispatch(call sdkgo.Call) bool {
	var marker legacySendDispatchMarker
	isMarkerFound, err := call.Context.GetLastHeartbeatValue(&marker)
	return err == nil && isMarkerFound && marker.DispatchedCallID == call.ID
}
