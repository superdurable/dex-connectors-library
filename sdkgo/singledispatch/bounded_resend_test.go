// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package singledispatch_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func newCappedFence(t *testing.T, ctx *singledispatchtest.Context, maxSends int32) *singledispatch.Fence {
	t.Helper()
	fence, err := singledispatch.NewFence(sdkgo.Call{Context: ctx, ID: "call-1"}, time.Second, singledispatch.WithMaxSends(maxSends))
	require.NoError(t, err)
	return fence
}

func contextAtAttempt(t *testing.T, attempt int32, marker *singledispatch.Marker) *singledispatchtest.Context {
	t.Helper()
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	for ctx.Attempt() < attempt {
		ctx.AdvanceAttempt()
	}
	if marker != nil {
		require.NoError(t, ctx.SetPersistedMarkers(marker))
	}
	return ctx
}

func marker(attempt int32, state singledispatch.State, sendCount int32) *singledispatch.Marker {
	return &singledispatch.Marker{CallID: "call-1", Attempt: attempt, State: state, SendCount: sendCount}
}

func TestBoundedResendComputesPriorSendUpperBound(t *testing.T) {
	const (
		resend   = singledispatch.ReadBackThenResendAllowed
		readBack = singledispatch.ReadBackRequired
		send     = singledispatch.SendAllowed
	)
	for _, scenario := range []struct {
		name           string
		attempt        int32
		marker         *singledispatch.Marker
		maxSends       int32
		wantKind       singledispatch.DecisionKind
		wantPriorSends int32
	}{
		{name: "no marker on attempt one", attempt: 1, maxSends: 3, wantKind: send, wantPriorSends: 0},
		{name: "no marker on a later attempt counts each earlier attempt", attempt: 3, maxSends: 3, wantKind: resend, wantPriorSends: 2},
		{name: "no marker at the cap", attempt: 4, maxSends: 3, wantKind: readBack, wantPriorSends: 3},
		{name: "immediate claim with a count", attempt: 3, marker: marker(2, singledispatch.StateClaimed, 1), maxSends: 3, wantKind: resend, wantPriorSends: 1},
		{name: "stale claim adds the attempts after it", attempt: 4, marker: marker(2, singledispatch.StateClaimed, 2), maxSends: 3, wantKind: readBack, wantPriorSends: 3},
		{name: "stale notSent with a count", attempt: 5, marker: marker(3, singledispatch.StateNotSent, 1), maxSends: 3, wantKind: resend, wantPriorSends: 2},
		{name: "immediate notSent preserves earlier sends", attempt: 5, marker: marker(4, singledispatch.StateNotSent, 2), maxSends: 3, wantKind: resend, wantPriorSends: 2},
		{name: "immediate canResend sends whatever its count", attempt: 4, marker: marker(3, singledispatch.StateCanResend, 2), maxSends: 2, wantKind: send, wantPriorSends: 2},
		{name: "legacy claim counts its attempt", attempt: 3, marker: marker(2, singledispatch.StateClaimed, 0), maxSends: 3, wantKind: resend, wantPriorSends: 2},
		{name: "legacy claim at a lower cap", attempt: 3, marker: marker(2, singledispatch.StateClaimed, 0), maxSends: 2, wantKind: readBack, wantPriorSends: 2},
		{name: "legacy stale notSent counts earlier attempts", attempt: 5, marker: marker(3, singledispatch.StateNotSent, 0), maxSends: 3, wantKind: readBack, wantPriorSends: 3},
		{name: "legacy stale notSent below a higher cap", attempt: 5, marker: marker(3, singledispatch.StateNotSent, 0), maxSends: 4, wantKind: resend, wantPriorSends: 3},
		{name: "legacy stale canResend", attempt: 4, marker: marker(2, singledispatch.StateCanResend, 0), maxSends: 3, wantKind: resend, wantPriorSends: 2},
		{name: "marker from the current attempt never resends", attempt: 2, marker: marker(2, singledispatch.StateNotSent, 0), maxSends: 3, wantKind: readBack, wantPriorSends: 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			decision := newCappedFence(t, contextAtAttempt(t, scenario.attempt, scenario.marker), scenario.maxSends).Decide()
			require.Equal(t, scenario.wantKind, decision.Kind)
			require.Equal(t, scenario.wantPriorSends, decision.MaximumPriorSends)
			require.NoError(t, decision.Err)
		})
	}
}

func TestBoundedResendReadsBackProviderIDBeforeResending(t *testing.T) {
	accepted := &singledispatch.Marker{CallID: "call-1", Attempt: 1, State: singledispatch.StateClaimed, ProviderID: "object-1", SendCount: 1}
	decision := newCappedFence(t, contextAtAttempt(t, 2, accepted), 3).Decide()
	require.Equal(t, singledispatch.ReadBackThenResendAllowed, decision.Kind)
	require.Equal(t, "object-1", decision.Marker.ProviderID, "the connector reads back the saved identity first")

	atCap := &singledispatch.Marker{CallID: "call-1", Attempt: 3, State: singledispatch.StateClaimed, ProviderID: "object-1", SendCount: 3}
	decision = newCappedFence(t, contextAtAttempt(t, 4, atCap), 3).Decide()
	require.Equal(t, singledispatch.ReadBackRequired, decision.Kind)
	require.Equal(t, "object-1", decision.Marker.ProviderID)
}

func TestBoundedResendIgnoresForeignOrUnreadableMarkers(t *testing.T) {
	for name, checkpoint := range map[string]any{
		"foreign":                 singledispatch.Marker{CallID: "call-2", Attempt: 1, State: singledispatch.StateNotSent},
		"unknown state":           map[string]any{"callId": "call-1", "attempt": 1, "state": "unknown"},
		"negative count":          map[string]any{"callId": "call-1", "attempt": 2, "state": "claimed", "sendCount": -1},
		"count of the wrong type": map[string]any{"callId": "call-1", "attempt": 1, "state": "claimed", "sendCount": "1"},
		"unknown field":           map[string]any{"callId": "call-1", "attempt": 1, "state": "notSent", "extra": true},
		"null provider ID":        map[string]any{"callId": "call-1", "attempt": 1, "state": "notSent", "providerId": nil},
		"string":                  "not a checkpoint",
		"map entry under another key": map[string]any{"singleDispatchMarkers": map[string]any{
			"call-2": map[string]any{"callId": "call-1", "attempt": 2, "state": "notSent"},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := contextAtAttempt(t, 3, nil)
			require.NoError(t, ctx.SetPersistedCheckpoint(checkpoint))
			fence := newCappedFence(t, ctx, 10)
			decision := fence.Decide()
			require.Equal(t, singledispatch.ReadBackThenResendAllowed, decision.Kind)
			require.NoError(t, decision.Err)
			require.Empty(t, decision.Marker)
			require.Equal(t, int32(2), decision.MaximumPriorSends, "no marker assumes each earlier attempt sent")
		})
	}
}

func TestDecideReadsLegacyOwnMarker(t *testing.T) {
	for name, testCase := range map[string]struct {
		checkpoint          any
		maxSends            int32
		wantKind            singledispatch.DecisionKind
		wantPriorSends      int32
		wantProviderID      string
		wantMarkerSendCount int32
	}{
		"immediate notSent retains earlier send": {
			checkpoint: singledispatch.Marker{CallID: "call-1", Attempt: 2, State: singledispatch.StateNotSent, SendCount: 1},
			maxSends:   1, wantKind: singledispatch.ReadBackRequired, wantPriorSends: 1, wantMarkerSendCount: 1,
		},
		"accepted claim keeps its provider ID": {
			checkpoint: singledispatch.Marker{CallID: "call-1", Attempt: 2, State: singledispatch.StateClaimed, ProviderID: "object-1", SendCount: 1},
			maxSends:   3, wantKind: singledispatch.ReadBackThenResendAllowed, wantPriorSends: 1, wantProviderID: "object-1", wantMarkerSendCount: 1,
		},
		"accepted claim at cap one requires read-back": {
			checkpoint: singledispatch.Marker{CallID: "call-1", Attempt: 2, State: singledispatch.StateClaimed, ProviderID: "object-1", SendCount: 1},
			maxSends:   1, wantKind: singledispatch.ReadBackRequired, wantPriorSends: 1, wantProviderID: "object-1", wantMarkerSendCount: 1,
		},
		"claim without a count counts its attempt": {
			checkpoint: map[string]any{"callId": "call-1", "attempt": 2, "state": "claimed"},
			maxSends:   3, wantKind: singledispatch.ReadBackThenResendAllowed, wantPriorSends: 2, wantMarkerSendCount: 2,
		},
		"stale canResend without a count excludes its own send": {
			checkpoint: map[string]any{"callId": "call-1", "attempt": 1, "state": "canResend"},
			maxSends:   2, wantKind: singledispatch.ReadBackThenResendAllowed, wantPriorSends: 1, wantMarkerSendCount: 0,
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := contextAtAttempt(t, 3, nil)
			require.NoError(t, ctx.SetPersistedCheckpoint(testCase.checkpoint))
			decision := newCappedFence(t, ctx, testCase.maxSends).Decide()
			require.Equal(t, testCase.wantKind, decision.Kind)
			require.NoError(t, decision.Err)
			require.Equal(t, sdkgo.CallID("call-1"), decision.Marker.CallID)
			require.Equal(t, testCase.wantPriorSends, decision.MaximumPriorSends)
			require.Equal(t, testCase.wantProviderID, decision.Marker.ProviderID)
			require.Equal(t, testCase.wantMarkerSendCount, decision.Marker.SendCount)
		})
	}
}

func TestLegacyOwnMarkerSurvivesAnotherCallsHeartbeat(t *testing.T) {
	ctx := contextAtAttempt(t, 2, nil)
	require.NoError(t, ctx.SetPersistedCheckpoint(singledispatch.Marker{
		CallID: "call-1", Attempt: 1, State: singledispatch.StateClaimed, ProviderID: "object-1", SendCount: 1,
	}))
	other, err := singledispatch.NewFence(sdkgo.Call{Context: ctx, ID: "call-2"}, time.Second, singledispatch.WithMaxSends(3))
	require.NoError(t, err)
	require.Equal(t, singledispatch.ReadBackThenResendAllowed, other.Decide().Kind)
	require.NoError(t, other.Dispatch(func(context.Context) error { return nil }))
	// call-1 reads back and records nothing in attempt two.
	require.Equal(t, "object-1", newCappedFence(t, ctx, 3).Decide().Marker.ProviderID)

	ctx.AdvanceAttempt()
	decision := newCappedFence(t, ctx, 3).Decide()
	require.Equal(t, singledispatch.ReadBackThenResendAllowed, decision.Kind)
	require.Equal(t, "object-1", decision.Marker.ProviderID)
	require.Equal(t, int32(2), decision.MaximumPriorSends)
}

func TestBoundedResendFailsClosedOnImpossibleOwnMarkers(t *testing.T) {
	for name, checkpoint := range map[string]any{
		"claim above its attempt":   map[string]any{"callId": "call-1", "attempt": 2, "state": "claimed", "sendCount": 3},
		"notSent counting itself":   map[string]any{"callId": "call-1", "attempt": 2, "state": "notSent", "sendCount": 2},
		"canResend counting itself": map[string]any{"callId": "call-1", "attempt": 1, "state": "canResend", "sendCount": 1},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := contextAtAttempt(t, 3, nil)
			require.NoError(t, ctx.SetPersistedMarkers(checkpoint))
			fence := newCappedFence(t, ctx, 10)
			decision := fence.Decide()
			require.Equal(t, singledispatch.FailClosed, decision.Kind)
			require.Error(t, decision.Err)
			require.Zero(t, decision.MaximumPriorSends)
			require.ErrorIs(t, fence.Dispatch(func(context.Context) error { t.Fatal("sent after an untrusted checkpoint"); return nil }), singledispatch.ErrDispatchNotAllowed)
		})
	}
}

// The same vectors run in sdkpython and sdkts to keep the count validity rule identical.
func TestBoundedResendValidatesMarkerSendCountRange(t *testing.T) {
	const (
		resend     = singledispatch.ReadBackThenResendAllowed
		readBack   = singledispatch.ReadBackRequired
		send       = singledispatch.SendAllowed
		failClosed = singledispatch.FailClosed
	)
	for _, scenario := range []struct {
		name           string
		attempt        int32
		marker         *singledispatch.Marker
		maxSends       int32
		wantKind       singledispatch.DecisionKind
		wantPriorSends int32
	}{
		{name: "claimed counting every attempt", attempt: 3, marker: marker(2, singledispatch.StateClaimed, 2), maxSends: 3, wantKind: resend, wantPriorSends: 2},
		{name: "notSent counting every earlier attempt", attempt: 3, marker: marker(2, singledispatch.StateNotSent, 1), maxSends: 3, wantKind: resend, wantPriorSends: 1},
		{name: "canResend counting every earlier attempt", attempt: 3, marker: marker(2, singledispatch.StateCanResend, 1), maxSends: 3, wantKind: send, wantPriorSends: 1},
		{name: "stale notSent at its maximum count", attempt: 4, marker: marker(2, singledispatch.StateNotSent, 1), maxSends: 3, wantKind: resend, wantPriorSends: 2},
		{name: "claimed above its attempt", attempt: 3, marker: marker(2, singledispatch.StateClaimed, 3), maxSends: 3, wantKind: failClosed},
		{name: "notSent counting its own attempt", attempt: 3, marker: marker(2, singledispatch.StateNotSent, 2), maxSends: 3, wantKind: failClosed},
		{name: "canResend counting its own attempt", attempt: 3, marker: marker(2, singledispatch.StateCanResend, 2), maxSends: 3, wantKind: failClosed},
		{name: "first-attempt notSent with a send", attempt: 2, marker: marker(1, singledispatch.StateNotSent, 1), maxSends: 10, wantKind: failClosed},
		{name: "first-attempt canResend with a send", attempt: 2, marker: marker(1, singledispatch.StateCanResend, 1), maxSends: 10, wantKind: failClosed},
		{name: "current-attempt claim above its attempt", attempt: 3, marker: marker(3, singledispatch.StateClaimed, 4), maxSends: 10, wantKind: failClosed},
		{name: "invalid count without bounded resend", attempt: 3, marker: marker(2, singledispatch.StateNotSent, 2), maxSends: 1, wantKind: failClosed},
		{name: "valid count without bounded resend", attempt: 3, marker: marker(2, singledispatch.StateClaimed, 2), maxSends: 1, wantKind: readBack, wantPriorSends: 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			decision := newCappedFence(t, contextAtAttempt(t, scenario.attempt, scenario.marker), scenario.maxSends).Decide()
			require.Equal(t, scenario.wantKind, decision.Kind)
			require.Equal(t, scenario.wantPriorSends, decision.MaximumPriorSends)
			if scenario.wantKind == failClosed {
				require.EqualError(t, decision.Err, "invalid single-dispatch checkpoint")
				require.Equal(t, *scenario.marker, decision.Marker)
			} else {
				require.NoError(t, decision.Err)
			}
		})
	}
}

func TestBoundedResendMarkersCarrySendCounts(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	first := newCappedFence(t, ctx, 3)
	require.Equal(t, singledispatch.SendAllowed, first.Decide().Kind)
	require.NoError(t, first.Dispatch(func(context.Context) error { return nil }))
	requireLastMarker(t, ctx, singledispatch.Marker{CallID: "call-1", Attempt: 1, State: singledispatch.StateClaimed, SendCount: 1})

	ctx.AdvanceAttempt()
	second := newCappedFence(t, ctx, 3)
	decision := second.Decide()
	require.Equal(t, singledispatch.ReadBackThenResendAllowed, decision.Kind)
	require.Equal(t, int32(1), decision.MaximumPriorSends)
	require.NoError(t, second.Dispatch(func(context.Context) error { return nil }))
	requireLastMarker(t, ctx, singledispatch.Marker{CallID: "call-1", Attempt: 2, State: singledispatch.StateClaimed, SendCount: 2})
	require.NoError(t, second.RecordCanResend())
	requireLastMarker(t, ctx, singledispatch.Marker{CallID: "call-1", Attempt: 2, State: singledispatch.StateCanResend, SendCount: 1})

	ctx.AdvanceAttempt()
	third := newCappedFence(t, ctx, 3)
	require.Equal(t, singledispatch.SendAllowed, third.Decide().Kind)
	require.NoError(t, third.Dispatch(func(context.Context) error { return nil }))
	require.NoError(t, third.RecordProviderID("object-1"))
	requireLastMarker(t, ctx, singledispatch.Marker{CallID: "call-1", Attempt: 3, State: singledispatch.StateClaimed, ProviderID: "object-1", SendCount: 2})

	ctx.AdvanceAttempt()
	fourth := newCappedFence(t, ctx, 3)
	decision = fourth.Decide()
	require.Equal(t, singledispatch.ReadBackThenResendAllowed, decision.Kind)
	require.Equal(t, "object-1", decision.Marker.ProviderID)
	require.NoError(t, fourth.Dispatch(func(context.Context) error { return nil }))
	requireLastMarker(t, ctx, singledispatch.Marker{CallID: "call-1", Attempt: 4, State: singledispatch.StateClaimed, SendCount: 3})

	ctx.AdvanceAttempt()
	fifth := newCappedFence(t, ctx, 3)
	require.Equal(t, singledispatch.ReadBackRequired, fifth.Decide().Kind, "three sends may have taken effect")
	require.ErrorIs(t, fifth.RecordNotSent(), singledispatch.ErrDispatchNotAllowed)
}

func TestBoundedResendPreSendRetryPreservesEarlierSend(t *testing.T) {
	ctx := contextAtAttempt(t, 2, marker(1, singledispatch.StateClaimed, 1))
	fence := newCappedFence(t, ctx, 3)
	require.Equal(t, singledispatch.ReadBackThenResendAllowed, fence.Decide().Kind)
	require.NoError(t, fence.RecordNotSent())
	requireLastMarker(t, ctx, singledispatch.Marker{CallID: "call-1", Attempt: 2, State: singledispatch.StateNotSent, SendCount: 1})
	require.ErrorIs(t, fence.Dispatch(func(context.Context) error { return nil }), singledispatch.ErrDispatchNotAllowed)
	ctx.AdvanceAttempt()
	next := newCappedFence(t, ctx, 3)
	require.Equal(t, singledispatch.ReadBackThenResendAllowed, next.Decide().Kind)
	require.Equal(t, int32(1), next.Decide().MaximumPriorSends)
}

func TestAcceptedWithoutIDNeverPermitsResend(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	fence := newCappedFence(t, ctx, 3)
	require.Equal(t, singledispatch.SendAllowed, fence.Decide().Kind)
	require.NoError(t, fence.Dispatch(func(context.Context) error { return nil }))
	require.NoError(t, fence.RecordAccepted())
	requireLastMarker(t, ctx, singledispatch.Marker{CallID: "call-1", Attempt: 1, State: singledispatch.StateAccepted, SendCount: 1})
	require.ErrorIs(t, fence.RecordCanResend(), singledispatch.ErrDispatchNotAllowed)
	ctx.AdvanceAttempt()
	require.Equal(t, singledispatch.ReadBackRequired, newCappedFence(t, ctx, 3).Decide().Kind)
}

func TestAcceptedWithoutIDCheckpointFailureDoesNotPermitResend(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	fence := newCappedFence(t, ctx, 3)
	fence.Decide()
	require.NoError(t, fence.Dispatch(func(context.Context) error { return nil }))
	ctx.SetWriteError(errors.New("heartbeat unavailable"))
	require.Error(t, fence.RecordAccepted())
	require.ErrorIs(t, fence.RecordCanResend(), singledispatch.ErrDispatchNotAllowed)
}

func TestBoundedResendPreSendRetryKeepsReadBack(t *testing.T) {
	claimed := marker(1, singledispatch.StateClaimed, 1)
	ctx := contextAtAttempt(t, 2, claimed)
	fence := newCappedFence(t, ctx, 3)
	require.Equal(t, singledispatch.ReadBackThenResendAllowed, fence.Decide().Kind)
	ctx.SetWriteError(errors.New("heartbeat unavailable"))
	require.ErrorIs(t, fence.Dispatch(func(context.Context) error { t.Fatal("sent after checkpoint failure"); return nil }), singledispatch.ErrCheckpoint)
	ctx.SetWriteError(nil)
	require.Empty(t, ctx.Heartbeats(), "both claim and best-effort proof failed")
	ctx.AdvanceAttempt()
	decision := newCappedFence(t, ctx, 3).Decide()
	require.Equal(t, singledispatch.ReadBackThenResendAllowed, decision.Kind)
	require.Equal(t, int32(2), decision.MaximumPriorSends)

	short := contextAtAttempt(t, 2, claimed)
	shortFence, err := singledispatch.NewFence(sdkgo.Call{Context: short, ID: "call-1"}, 20*time.Millisecond, singledispatch.WithMaxSends(3))
	require.NoError(t, err)
	require.Equal(t, singledispatch.ReadBackThenResendAllowed, shortFence.Decide().Kind)
	time.Sleep(15 * time.Millisecond) // Elapsed time is the behavior under test: the write window closes.
	require.ErrorIs(t, shortFence.Dispatch(func(context.Context) error { t.Fatal("sent after write window"); return nil }), singledispatch.ErrInsufficientTime)
	requireLastMarker(t, short, singledispatch.Marker{CallID: "call-1", Attempt: 2, State: singledispatch.StateNotSent, SendCount: 1})
	require.ErrorIs(t, shortFence.Dispatch(func(context.Context) error { t.Fatal("sent twice"); return nil }), singledispatch.ErrDispatchNotAllowed)
}

func TestBoundedResendStopsAtTheCap(t *testing.T) {
	for _, maxSends := range []int32{2, 3, 5} {
		ctx := singledispatchtest.NewContext("flow-1", "step-1")
		sends := int32(0)
		for range 12 {
			fence := newCappedFence(t, ctx, maxSends)
			if decision := fence.Decide(); decision.Kind == singledispatch.SendAllowed || decision.Kind == singledispatch.ReadBackThenResendAllowed {
				// Every send times out, so each outcome stays unknown.
				require.ErrorIs(t, fence.Dispatch(func(context.Context) error { sends++; return context.DeadlineExceeded }), context.DeadlineExceeded)
			}
			ctx.AdvanceAttempt()
		}
		require.Equal(t, maxSends, sends)
	}
}

func TestBoundedResendCountsLostClaimsByAttempt(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	sends := 0
	for range 6 {
		fence := newCappedFence(t, ctx, 3)
		if decision := fence.Decide(); decision.Kind == singledispatch.SendAllowed || decision.Kind == singledispatch.ReadBackThenResendAllowed {
			ctx.LoseNextHeartbeat()
			require.NoError(t, fence.Dispatch(func(context.Context) error { sends++; return nil }))
		}
		ctx.AdvanceAttempt()
	}
	require.Equal(t, 3, sends, "with every claim lost, each earlier attempt still counts as one send")
}

func TestMaxSendsOneKeepsTodaysDecisions(t *testing.T) {
	checkpoints := []*singledispatch.Marker{
		nil,
		marker(1, singledispatch.StateNotSent, 0),
		marker(1, singledispatch.StateCanResend, 0),
		marker(1, singledispatch.StateClaimed, 0),
		marker(1, singledispatch.StateClaimed, 1),
		{CallID: "call-1", Attempt: 1, State: singledispatch.StateClaimed, ProviderID: "object-1"},
		{CallID: "call-1", Attempt: 1, State: singledispatch.StateCanResend, ProviderID: "object-1"},
		marker(2, singledispatch.StateNotSent, 0),
		marker(2, singledispatch.StateClaimed, 0),
		{CallID: "call-2", Attempt: 1, State: singledispatch.StateNotSent},
	}
	for _, attempt := range []int32{1, 2, 3, 4} {
		for _, checkpoint := range checkpoints {
			byDefault := newFence(t, contextAtAttempt(t, attempt, checkpoint)).Decide()
			withOne := newCappedFence(t, contextAtAttempt(t, attempt, checkpoint), 1).Decide()
			require.Equal(t, byDefault.Kind, withOne.Kind, "attempt %d marker %+v", attempt, checkpoint)
			require.NotEqual(t, singledispatch.ReadBackThenResendAllowed, withOne.Kind)
		}
	}
	for _, scenario := range []struct {
		attempt int32
		marker  *singledispatch.Marker
		want    singledispatch.DecisionKind
	}{
		{attempt: 1, want: singledispatch.SendAllowed},
		{attempt: 2, want: singledispatch.ReadBackRequired},
		{attempt: 2, marker: marker(1, singledispatch.StateNotSent, 0), want: singledispatch.SendAllowed},
		{attempt: 3, marker: marker(1, singledispatch.StateCanResend, 0), want: singledispatch.ReadBackRequired},
		{attempt: 2, marker: marker(1, singledispatch.StateClaimed, 1), want: singledispatch.ReadBackRequired},
	} {
		require.Equal(t, scenario.want, newFence(t, contextAtAttempt(t, scenario.attempt, scenario.marker)).Decide().Kind)
	}

	for _, options := range [][]singledispatch.FenceOption{nil, {singledispatch.WithMaxSends(1)}} {
		ctx := singledispatchtest.NewContext("flow-1", "step-1")
		fence, err := singledispatch.NewFence(sdkgo.Call{Context: ctx, ID: "call-1"}, time.Second, options...)
		require.NoError(t, err)
		require.Equal(t, singledispatch.SendAllowed, fence.Decide().Kind)
		require.NoError(t, fence.Dispatch(func(context.Context) error { return nil }))
		require.NoError(t, fence.RecordCanResend())
		ctx.AdvanceAttempt()
		second, err := singledispatch.NewFence(sdkgo.Call{Context: ctx, ID: "call-1"}, time.Second, options...)
		require.NoError(t, err)
		require.Equal(t, singledispatch.SendAllowed, second.Decide().Kind)
		require.NoError(t, second.Dispatch(func(context.Context) error { return nil }))
		require.NoError(t, second.RecordProviderID("object-1"))
		frames := make([]string, 0, 4)
		for _, frame := range ctx.Heartbeats() {
			frames = append(frames, string(frame))
		}
		require.Equal(t, []string{
			`{"singleDispatchMarkers":{"call-1":{"callId":"call-1","attempt":1,"state":"claimed","sendCount":1}}}`,
			`{"singleDispatchMarkers":{"call-1":{"callId":"call-1","attempt":1,"state":"canResend","sendCount":0}}}`,
			`{"singleDispatchMarkers":{"call-1":{"callId":"call-1","attempt":2,"state":"claimed","sendCount":1}}}`,
			`{"singleDispatchMarkers":{"call-1":{"callId":"call-1","attempt":2,"state":"claimed","providerId":"object-1","sendCount":1}}}`,
		}, frames, "markers without bounded resend also write the exact sendCount")
	}
}

func TestMaxSendsOneCountsRefusedSendsExactly(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	for range 2 {
		fence, err := singledispatch.NewFence(sdkgo.Call{Context: ctx, ID: "call-1"}, time.Second, singledispatch.WithMaxSends(1))
		require.NoError(t, err)
		decision := fence.Decide()
		require.Equal(t, singledispatch.SendAllowed, decision.Kind)
		require.Zero(t, decision.MaximumPriorSends)
		require.NoError(t, fence.Dispatch(func(context.Context) error { return nil }))
		require.NoError(t, fence.RecordCanResend(), "a 429 proves the send was not processed")
		ctx.AdvanceAttempt()
	}
	third, err := singledispatch.NewFence(sdkgo.Call{Context: ctx, ID: "call-1"}, time.Second, singledispatch.WithMaxSends(1))
	require.NoError(t, err)
	decision := third.Decide()
	require.Equal(t, singledispatch.SendAllowed, decision.Kind)
	require.Zero(t, decision.MaximumPriorSends, "two refused sends leave no possible earlier send, so a 422 is a definite rejection")
}

func TestCheckpointEntryWithoutSendCountIsCountedConservatively(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	require.NoError(t, ctx.SetPersistedCheckpoint(map[string]any{"singleDispatchMarkers": map[string]any{
		"call-1": map[string]any{"callId": "call-1", "attempt": 2, "state": "canResend"},
	}}))
	ctx.AdvanceAttempt()
	ctx.AdvanceAttempt()
	decision := newFence(t, ctx).Decide()
	require.Equal(t, singledispatch.SendAllowed, decision.Kind)
	require.Equal(t, int32(1), decision.MaximumPriorSends)
	require.Equal(t, int32(1), decision.Marker.SendCount)
}

func TestClaimedCheckpointEntryWithZeroSendCountFailsClosed(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	require.NoError(t, ctx.SetPersistedCheckpoint(map[string]any{"singleDispatchMarkers": map[string]any{
		"call-1": map[string]any{"callId": "call-1", "attempt": 1, "state": "claimed", "sendCount": 0},
	}}))
	ctx.AdvanceAttempt()
	fence, err := singledispatch.NewFence(sdkgo.Call{Context: ctx, ID: "call-1"}, time.Second, singledispatch.WithMaxSends(3))
	require.NoError(t, err)
	require.Equal(t, singledispatch.FailClosed, fence.Decide().Kind)
}

func TestWithMaxSendsRange(t *testing.T) {
	for _, maxSends := range []int32{0, -1, sdkgo.MaxSendsCeiling + 1} {
		_, err := singledispatch.NewFence(sdkgo.Call{Context: singledispatchtest.NewContext("flow-1", "step-1"), ID: "call-1"}, time.Second, singledispatch.WithMaxSends(maxSends))
		require.Error(t, err, maxSends)
	}
	for _, maxSends := range []int32{1, 2, sdkgo.MaxSendsCeiling} {
		_, err := singledispatch.NewFence(sdkgo.Call{Context: singledispatchtest.NewContext("flow-1", "step-1"), ID: "call-1"}, time.Second, singledispatch.WithMaxSends(maxSends))
		require.NoError(t, err, maxSends)
	}
}

func requireLastMarker(t *testing.T, ctx *singledispatchtest.Context, want singledispatch.Marker) {
	t.Helper()
	heartbeats := ctx.Heartbeats()
	require.NotEmpty(t, heartbeats)
	var got struct {
		SingleDispatchMarkers map[sdkgo.CallID]singledispatch.Marker `json:"singleDispatchMarkers"`
	}
	require.NoError(t, json.Unmarshal(heartbeats[len(heartbeats)-1], &got))
	require.Equal(t, want, got.SingleDispatchMarkers[want.CallID])
}
