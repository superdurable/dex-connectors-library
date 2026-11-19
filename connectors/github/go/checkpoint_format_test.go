// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package github_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// checkpointMarkersField is the heartbeat key of SDK releases that map each Call ID to its Marker.
const checkpointMarkersField = "singleDispatchMarkers"

// decodeRecordedMarker returns the only Marker in a heartbeat frame of either checkpoint format.
func decodeRecordedMarker(t *testing.T, frame []byte) singledispatch.Marker {
	t.Helper()
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(frame, &fields))
	encodedMarkers, hasMarkersByCallID := fields[checkpointMarkersField]
	if !hasMarkersByCallID {
		var marker singledispatch.Marker
		require.NoError(t, json.Unmarshal(frame, &marker))
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
