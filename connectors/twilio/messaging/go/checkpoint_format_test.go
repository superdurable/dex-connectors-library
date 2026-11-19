// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package messaging_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

// checkpointMarkersField is the heartbeat key of SDK releases that map each Call ID to its Marker.
const checkpointMarkersField = "singleDispatchMarkers"

// readPersistedMarker returns the only Marker in the heartbeat visible to the current attempt.
func readPersistedMarker(t *testing.T, ctx *singledispatchtest.Context) singledispatch.Marker {
	t.Helper()
	var fields map[string]json.RawMessage
	hasHeartbeat, err := ctx.GetLastHeartbeatValue(&fields)
	require.NoError(t, err)
	require.True(t, hasHeartbeat)
	encodedMarkers, hasMarkersByCallID := fields[checkpointMarkersField]
	if !hasMarkersByCallID {
		var marker singledispatch.Marker
		_, err := ctx.GetLastHeartbeatValue(&marker)
		require.NoError(t, err)
		return marker
	}
	var markersByCallID map[sdkgo.CallID]singledispatch.Marker
	require.NoError(t, json.Unmarshal(encodedMarkers, &markersByCallID))
	require.Len(t, markersByCallID, 1, "the heartbeat holds one Call's Marker")
	for _, marker := range markersByCallID {
		return marker
	}
	return singledispatch.Marker{}
}
