// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package singledispatch

import (
	"maps"
	"math"
	"sync"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/stepattempt"
	"github.com/superdurable/dex/sdk-go/dex"
)

// checkpointMarkersField names the heartbeat object that maps each Call ID to its Marker.
const checkpointMarkersField = "singleDispatchMarkers"

// checkpointHeartbeat is the heartbeat value every Fence records. The format is internal to
// the connector SDKs and may change.
type checkpointHeartbeat struct {
	SingleDispatchMarkers map[sdkgo.CallID]checkpointEntry `json:"singleDispatchMarkers"`
}

// checkpointEntry encodes a Marker in the checkpoint map with its exact send count, zero included.
type checkpointEntry struct {
	CallID     sdkgo.CallID `json:"callId"`
	Attempt    int32        `json:"attempt"`
	State      State        `json:"state"`
	ProviderID string       `json:"providerId,omitempty"`
	SendCount  int32        `json:"sendCount"`
}

// stepCheckpoint merges the Markers of every Fence in one Step attempt into each heartbeat.
type stepCheckpoint struct {
	mu      sync.Mutex
	markers map[sdkgo.CallID]Marker
}

var stepCheckpoints = stepattempt.NewRegistry(func(ctx dex.Context) *stepCheckpoint {
	return &stepCheckpoint{markers: previousAttemptMarkers(ctx)}
})

func stepCheckpointFor(ctx dex.Context) *stepCheckpoint {
	return stepCheckpoints.For(ctx)
}

// record holds the lock across the heartbeat so the last persisted value has every committed entry.
func (checkpoint *stepCheckpoint) record(ctx dex.Context, marker Marker) error {
	return checkpoint.doRecord(ctx, marker, false)
}

// recordRetainingOnFailure keeps marker even when its heartbeat fails; a kept provider ID only forces read-back.
func (checkpoint *stepCheckpoint) recordRetainingOnFailure(ctx dex.Context, marker Marker) error {
	return checkpoint.doRecord(ctx, marker, true)
}

func (checkpoint *stepCheckpoint) doRecord(ctx dex.Context, marker Marker, shouldRetainOnFailure bool) error {
	checkpoint.mu.Lock()
	defer checkpoint.mu.Unlock()
	markers := make(map[sdkgo.CallID]Marker, len(checkpoint.markers)+1)
	maps.Copy(markers, checkpoint.markers)
	markers[marker.CallID] = marker
	if err := ctx.RecordHeartbeat(encodeCheckpointHeartbeat(markers)); err != nil {
		if shouldRetainOnFailure {
			checkpoint.markers = markers
		}
		return err
	}
	checkpoint.markers = markers
	return nil
}

func encodeCheckpointHeartbeat(markers map[sdkgo.CallID]Marker) checkpointHeartbeat {
	entries := make(map[sdkgo.CallID]checkpointEntry, len(markers))
	for callID, marker := range markers {
		entries[callID] = checkpointEntry{
			CallID: marker.CallID, Attempt: marker.Attempt, State: marker.State,
			ProviderID: marker.ProviderID, SendCount: marker.SendCount,
		}
	}
	return checkpointHeartbeat{SingleDispatchMarkers: entries}
}

// previousAttemptMarkers seeds a checkpoint; an unreadable heartbeat seeds nothing, which only overcounts.
func previousAttemptMarkers(ctx dex.Context) map[sdkgo.CallID]Marker {
	var heartbeat any
	hasHeartbeat, err := ctx.GetLastHeartbeatValue(&heartbeat)
	if err != nil || !hasHeartbeat {
		return map[sdkgo.CallID]Marker{}
	}
	return markersFromHeartbeat(heartbeat)
}

// markersFromHeartbeat drops unreadable or mis-keyed entries; any non-map value, legacy Markers included, yields none.
func markersFromHeartbeat(heartbeat any) map[sdkgo.CallID]Marker {
	markers := map[sdkgo.CallID]Marker{}
	fields, _ := heartbeat.(map[string]any)
	entries, _ := fields[checkpointMarkersField].(map[string]any)
	for callID, entry := range entries {
		if marker, isMarker := markerFromValue(entry); isMarker && string(marker.CallID) == callID {
			markers[marker.CallID] = marker
		}
	}
	return markers
}

// markerFromValue is as strict as the Python and TypeScript decoders; Decide checks send counts.
// A present sendCount is exact; an absent one is counted conservatively.
func markerFromValue(value any) (Marker, bool) {
	fields, isObject := value.(map[string]any)
	if !isObject {
		return Marker{}, false
	}
	for name := range fields {
		switch name {
		case "callId", "attempt", "state", "providerId", "sendCount":
		default:
			return Marker{}, false
		}
	}
	callID, isCallID := fields["callId"].(string)
	state, isState := fields["state"].(string)
	attempt, isAttempt := int32Field(fields["attempt"])
	// NewFence accepts any nonempty Call ID, so a valid entry needs no UUID check here.
	if !isCallID || callID == "" || !isState || !isAttempt || attempt < 1 {
		return Marker{}, false
	}
	marker := Marker{CallID: sdkgo.CallID(callID), Attempt: attempt, State: State(state)}
	switch marker.State {
	case StateNotSent, StateClaimed, StateCanResend:
	default:
		return Marker{}, false
	}
	if providerIDValue, hasProviderID := fields["providerId"]; hasProviderID {
		providerID, isString := providerIDValue.(string)
		if !isString {
			return Marker{}, false
		}
		marker.ProviderID = providerID
	}
	sendCountValue, hasSendCount := fields["sendCount"]
	if !hasSendCount {
		marker.SendCount = conservativeSendCount(marker)
		return marker, true
	}
	sendCount, isInteger := int32Field(sendCountValue)
	if !isInteger || sendCount < 0 {
		return Marker{}, false
	}
	marker.SendCount = sendCount
	return marker, true
}

// conservativeSendCount counts one send per attempt, excluding a notSent or canResend marker's own.
func conservativeSendCount(marker Marker) int32 {
	if marker.State == StateClaimed {
		return marker.Attempt
	}
	return marker.Attempt - 1
}

func int32Field(value any) (int32, bool) {
	number, isNumber := value.(float64)
	if !isNumber || number != math.Trunc(number) || number < math.MinInt32 || number > math.MaxInt32 {
		return 0, false
	}
	return int32(number), true
}
