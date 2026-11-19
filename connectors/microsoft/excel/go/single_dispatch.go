// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel

import (
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// legacyDispatchMarker is the checkpoint that releases before v0.24.1 recorded before an append.
type legacyDispatchMarker struct {
	DispatchedCallID sdkgo.CallID `json:"microsoftExcelDispatchedCallId"`
}

// hasLegacyDispatchMarker reports whether an earlier attempt of an older release may have sent the append.
// Such an attempt kept no send count, so the append is read back and never resent.
func hasLegacyDispatchMarker(call sdkgo.Call) bool {
	var marker legacyDispatchMarker
	isMarkerFound, err := call.Context.GetLastHeartbeatValue(&marker)
	return err == nil && isMarkerFound && marker.DispatchedCallID == call.ID
}
