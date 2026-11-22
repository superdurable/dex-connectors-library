// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package singledispatch

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// priorCallEntry is one Call ID's entry in the earlier attempts' checkpoint, with its send bound.
type priorCallEntry struct {
	marker            Marker
	hasMarker         bool
	maximumPriorSends int32
}

// MaximumPriorSends returns an upper bound on the possibly effective sends that earlier
// attempts of ctx's Step made for callID. It equals the Decision.MaximumPriorSends that
// Decide of a Fence for callID returns in this attempt, because both count the same way:
// zero on attempt one without an entry; one per earlier attempt when the last heartbeat
// holds no readable entry for callID, also when no heartbeat was recorded; otherwise the
// entry's send count plus one per attempt after the entry's attempt. A claim counts its
// own send; an attempt that recorded canResend or notSent adds nothing.
//
// It needs no Fence, so an application Mutation or another wrapper can call it before the
// connector operation it wraps runs, naming that operation's call with sdkgo.NextCallID.
// When the wrapper's own pre-send check refuses the call and the result is above zero, an
// earlier send may have taken effect, so the wrapper selects uncertain, never a definite
// refusal. The read records no heartbeat, does not change a later Decide, and does not
// count sends of the current attempt. It is safe for concurrent callers.
//
// It returns zero and an error when ctx is nil, its attempt is below one, or callID is
// empty; when Dex cannot read the last heartbeat; and when callID's entry has a send count
// that one send per attempt could not produce. The last two are the errors Decide reports in
// Decision.Err when it fails closed. After an error the bound is unknown: never select a
// definite refusal; return Retry, as a connector does for a FailClosed decision.
func MaximumPriorSends(ctx dex.Context, callID sdkgo.CallID) (int32, error) {
	if ctx == nil || callID == "" || ctx.Attempt() < 1 {
		return 0, errors.New("maximum prior sends requires a Dex Step context, a positive attempt, and a Call ID")
	}
	entry, err := readPriorCallEntry(ctx, callID)
	if err != nil {
		return 0, err
	}
	return entry.maximumPriorSends, nil
}

// readPriorCallEntry is the prior-send count shared by Decide and MaximumPriorSends;
// an invalid entry keeps its Marker.
func readPriorCallEntry(ctx dex.Context, callID sdkgo.CallID) (priorCallEntry, error) {
	attempt := ctx.Attempt()
	var heartbeat any
	hasHeartbeat, err := ctx.GetLastHeartbeatValue(&heartbeat)
	if err != nil {
		return priorCallEntry{}, err
	}
	previous, hasPrevious := Marker{}, false
	if hasHeartbeat {
		previous, hasPrevious = markersFromHeartbeat(heartbeat)[callID]
	}
	if !hasPrevious {
		// Each earlier attempt sends at most once.
		return priorCallEntry{maximumPriorSends: attempt - 1}, nil
	}
	if !isValidMarker(previous) {
		return priorCallEntry{marker: previous, hasMarker: true}, errors.New("invalid single-dispatch checkpoint")
	}
	return priorCallEntry{marker: previous, hasMarker: true, maximumPriorSends: maximumPriorSends(previous, attempt)}, nil
}
