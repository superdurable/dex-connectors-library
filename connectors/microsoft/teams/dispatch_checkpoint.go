// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teams

import (
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// postDispatchCheckpoint is the Dex heartbeat checkpoint recorded just before a post is dispatched.
type postDispatchCheckpoint struct {
	DispatchedAtUnixMilli int64 `json:"teamsPostDispatchedAtUnixMilli"`
}

// readDispatchCheckpoint reports an earlier attempt's send time; an unreadable checkpoint still counts.
func readDispatchCheckpoint(call sdkgo.Call) (time.Time, bool) {
	var checkpoint postDispatchCheckpoint
	isFound, err := call.Context.GetLastHeartbeatValue(&checkpoint)
	if err != nil {
		return time.Time{}, true
	}
	if !isFound {
		return time.Time{}, false
	}
	if checkpoint.DispatchedAtUnixMilli <= 0 {
		return time.Time{}, true
	}
	return time.UnixMilli(checkpoint.DispatchedAtUnixMilli), true
}

// recordDispatchCheckpoint stores the checkpoint; the post may be dispatched only after Dex accepted it.
func recordDispatchCheckpoint(call sdkgo.Call, dispatchedAt time.Time) error {
	return call.Context.RecordHeartbeat(postDispatchCheckpoint{DispatchedAtUnixMilli: dispatchedAt.UnixMilli()})
}

// clearDispatchCheckpoint removes the checkpoint after Graph provably stored nothing.
func clearDispatchCheckpoint(call sdkgo.Call) {
	// A lost clear leaves the checkpoint, so the next attempt reads back instead of posting again.
	_ = call.Context.RecordHeartbeat(nil)
}

// earliestDispatchTime is the earlier of now and Dex's first attempt time for this Step execution.
func earliestDispatchTime(call sdkgo.Call, now time.Time) time.Time {
	return earlierTime(now, call.Context.FirstAttemptAt())
}

// earlierTime returns the earlier of two times, ignoring a zero one.
func earlierTime(current time.Time, candidate time.Time) time.Time {
	if !candidate.IsZero() && (current.IsZero() || candidate.Before(current)) {
		return candidate
	}
	return current
}
