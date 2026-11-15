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
		require.NoError(t, ctx.SetPersistedCheckpoint(marker))
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
		{name: "immediate notSent sends whatever its count", attempt: 5, marker: marker(4, singledispatch.StateNotSent, 2), maxSends: 3, wantKind: send, wantPriorSends: 2},
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

func TestBoundedResendFailsClosedOnForeignOrInvalidMarkers(t *testing.T) {
	for name, checkpoint := range map[string]any{
		"foreign":                   singledispatch.Marker{CallID: "call-2", Attempt: 1, State: singledispatch.StateNotSent},
		"unknown state":             map[string]any{"callId": "call-1", "attempt": 1, "state": "unknown"},
		"negative count":            map[string]any{"callId": "call-1", "attempt": 2, "state": "claimed", "sendCount": -1},
		"claim above its attempt":   map[string]any{"callId": "call-1", "attempt": 2, "state": "claimed", "sendCount": 3},
		"notSent counting itself":   map[string]any{"callId": "call-1", "attempt": 2, "state": "notSent", "sendCount": 2},
		"canResend counting itself": map[string]any{"callId": "call-1", "attempt": 1, "state": "canResend", "sendCount": 1},
		"count of the wrong type":   map[string]any{"callId": "call-1", "attempt": 1, "state": "claimed", "sendCount": "1"},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := contextAtAttempt(t, 3, nil)
			require.NoError(t, ctx.SetPersistedCheckpoint(checkpoint))
			fence := newCappedFence(t, ctx, 10)
			decision := fence.Decide()
			require.Equal(t, singledispatch.FailClosed, decision.Kind)
			require.Error(t, decision.Err)
			require.Zero(t, decision.MaximumPriorSends)
			require.ErrorIs(t, fence.Dispatch(func(context.Context) error { t.Fatal("sent after an untrusted checkpoint"); return nil }), singledispatch.ErrDispatchNotAllowed)
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
	heartbeatCount := len(ctx.Heartbeats())
	require.ErrorIs(t, fourth.RecordNotSent(), singledispatch.ErrDispatchNotAllowed, "notSent would let the next attempt skip read-back")
	require.Len(t, ctx.Heartbeats(), heartbeatCount)
	require.NoError(t, fourth.Dispatch(func(context.Context) error { return nil }))
	requireLastMarker(t, ctx, singledispatch.Marker{CallID: "call-1", Attempt: 4, State: singledispatch.StateClaimed, SendCount: 3})

	ctx.AdvanceAttempt()
	fifth := newCappedFence(t, ctx, 3)
	require.Equal(t, singledispatch.ReadBackRequired, fifth.Decide().Kind, "three sends may have taken effect")
	require.ErrorIs(t, fifth.RecordNotSent(), singledispatch.ErrDispatchNotAllowed)
}

func TestBoundedResendPreSendRetryKeepsReadBack(t *testing.T) {
	claimed := marker(1, singledispatch.StateClaimed, 1)
	ctx := contextAtAttempt(t, 2, claimed)
	fence := newCappedFence(t, ctx, 3)
	require.Equal(t, singledispatch.ReadBackThenResendAllowed, fence.Decide().Kind)
	ctx.SetWriteError(errors.New("heartbeat unavailable"))
	require.ErrorIs(t, fence.Dispatch(func(context.Context) error { t.Fatal("sent after checkpoint failure"); return nil }), singledispatch.ErrCheckpoint)
	ctx.SetWriteError(nil)
	require.Empty(t, ctx.Heartbeats(), "no notSent follows a possible earlier send")
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
	require.Empty(t, short.Heartbeats())
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
			`{"callId":"call-1","attempt":1,"state":"claimed"}`,
			`{"callId":"call-1","attempt":1,"state":"canResend"}`,
			`{"callId":"call-1","attempt":2,"state":"claimed"}`,
			`{"callId":"call-1","attempt":2,"state":"claimed","providerId":"object-1"}`,
		}, frames, "markers without bounded resend stay byte-identical to earlier releases")
	}
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
	var got singledispatch.Marker
	require.NoError(t, json.Unmarshal(heartbeats[len(heartbeats)-1], &got))
	require.Equal(t, want, got)
}
