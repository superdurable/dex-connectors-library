// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

// checkpointMarkersField is the heartbeat key of SDK releases that map each Call ID to its Marker.
const checkpointMarkersField = "singleDispatchMarkers"

// seedPersistedMarker seeds marker in the checkpoint format the linked SDK records.
func seedPersistedMarker(t *testing.T, ctx *singledispatchtest.Context, marker singledispatch.Marker) {
	t.Helper()
	if !linkedSDKRecordsMarkersByCallID(t) {
		require.NoError(t, ctx.SetPersistedCheckpoint(marker))
		return
	}
	require.NoError(t, ctx.SetPersistedCheckpoint(map[string]any{
		checkpointMarkersField: map[sdkgo.CallID]singledispatch.Marker{marker.CallID: marker},
	}))
}

// linkedSDKRecordsMarkersByCallID probes a scratch Fence because released SDKs record a bare Marker.
func linkedSDKRecordsMarkersByCallID(t *testing.T) bool {
	t.Helper()
	probeContext := singledispatchtest.NewContext("checkpoint-format-probe", "checkpoint-format-probe")
	fence, err := singledispatch.NewFence(sdkgo.Call{Context: probeContext, ID: "checkpoint-format-probe"}, time.Minute)
	require.NoError(t, err)
	require.Equal(t, singledispatch.SendAllowed, fence.Decide().Kind)
	require.NoError(t, fence.RecordNotSent())
	heartbeats := probeContext.Heartbeats()
	require.Len(t, heartbeats, 1)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(heartbeats[0], &fields))
	_, hasMarkersByCallID := fields[checkpointMarkersField]
	return hasMarkersByCallID
}
