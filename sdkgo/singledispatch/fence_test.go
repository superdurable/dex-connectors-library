// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package singledispatch_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func newFence(t *testing.T, ctx *singledispatchtest.Context) *singledispatch.Fence {
	t.Helper()
	fence, err := singledispatch.NewFence(sdkgo.Call{Context: ctx, ID: "call-1"}, time.Second)
	require.NoError(t, err)
	return fence
}

func TestDispatchDecisionOnlyImmediateProofPermitsSend(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		attempts int
		marker   *singledispatch.Marker
		want     singledispatch.DecisionKind
	}{
		{name: "first attempt without marker", want: singledispatch.SendAllowed},
		{name: "later attempt without marker", attempts: 1, want: singledispatch.ReadBackRequired},
		{name: "stale canResend", attempts: 2, marker: &singledispatch.Marker{CallID: "call-1", Attempt: 1, State: singledispatch.StateCanResend}, want: singledispatch.ReadBackRequired},
		{name: "immediate canResend", attempts: 1, marker: &singledispatch.Marker{CallID: "call-1", Attempt: 1, State: singledispatch.StateCanResend}, want: singledispatch.SendAllowed},
		{name: "immediate notSent", attempts: 1, marker: &singledispatch.Marker{CallID: "call-1", Attempt: 1, State: singledispatch.StateNotSent}, want: singledispatch.SendAllowed},
		{name: "claimed", attempts: 1, marker: &singledispatch.Marker{CallID: "call-1", Attempt: 1, State: singledispatch.StateClaimed}, want: singledispatch.ReadBackRequired},
		{name: "foreign", attempts: 1, marker: &singledispatch.Marker{CallID: "call-2", Attempt: 1, State: singledispatch.StateCanResend}, want: singledispatch.ReadBackRequired},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := singledispatchtest.NewContext("flow-1", "step-1")
			for range scenario.attempts {
				ctx.AdvanceAttempt()
			}
			if scenario.marker != nil {
				require.NoError(t, ctx.SetPersistedCheckpoint(scenario.marker))
			}
			fence := newFence(t, ctx)
			require.Equal(t, scenario.want, fence.Decide().Kind)
			sent := false
			err := fence.Dispatch(func(context.Context) error { sent = true; return nil })
			if scenario.want == singledispatch.SendAllowed {
				require.NoError(t, err)
				require.True(t, sent)
			} else {
				require.ErrorIs(t, err, singledispatch.ErrDispatchNotAllowed)
				require.False(t, sent)
			}
		})
	}
}

func Test503CanResendButLostNextClaimCannot(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	first := newFence(t, ctx)
	require.Equal(t, singledispatch.SendAllowed, first.Decide().Kind)
	require.NoError(t, first.Dispatch(func(context.Context) error { return nil }))
	require.NoError(t, first.RecordCanResend())
	ctx.AdvanceAttempt()
	second := newFence(t, ctx)
	require.Equal(t, singledispatch.SendAllowed, second.Decide().Kind)
	ctx.LoseNextHeartbeat()
	sends := 0
	require.NoError(t, second.Dispatch(func(context.Context) error { sends++; return nil }))
	ctx.AdvanceAttempt()
	third := newFence(t, ctx)
	decision := third.Decide()
	require.Equal(t, singledispatch.ReadBackRequired, decision.Kind)
	require.Equal(t, int32(1), decision.Marker.Attempt, "the older canResend marker survives the lost claim")
	require.ErrorIs(t, third.Dispatch(func(context.Context) error { sends++; return nil }), singledispatch.ErrDispatchNotAllowed)
	require.Equal(t, 1, sends)
}

func TestPreSendRetryRecordsNotSentForNextAttempt(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	first := newFence(t, ctx)
	require.Equal(t, singledispatch.SendAllowed, first.Decide().Kind)
	require.Empty(t, ctx.Heartbeats(), "decision and credential lookup cannot grant a later send")
	require.NoError(t, first.RecordNotSent())
	require.ErrorIs(t, first.Dispatch(func(context.Context) error { t.Fatal("sent after notSent Retry"); return nil }), singledispatch.ErrDispatchNotAllowed)
	ctx.AdvanceAttempt()
	second := newFence(t, ctx)
	require.Equal(t, singledispatch.SendAllowed, second.Decide().Kind)
	require.NoError(t, second.Dispatch(func(context.Context) error { return nil }))
}

func TestClaimBlocksCredentialFailureOnNextAttempt(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	first := newFence(t, ctx)
	require.Equal(t, singledispatch.SendAllowed, first.Decide().Kind)
	require.NoError(t, first.Dispatch(func(context.Context) error { return nil }))
	ctx.AdvanceAttempt()
	second := newFence(t, ctx)
	require.Equal(t, singledispatch.ReadBackRequired, second.Decide().Kind)
	require.ErrorIs(t, second.RecordNotSent(), singledispatch.ErrDispatchNotAllowed)
	require.ErrorIs(t, second.Dispatch(func(context.Context) error { t.Fatal("sent after claim"); return nil }), singledispatch.ErrDispatchNotAllowed)
}

func TestAcceptedProviderIDRequiresReadBack(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	first := newFence(t, ctx)
	require.Equal(t, singledispatch.SendAllowed, first.Decide().Kind)
	require.NoError(t, first.Dispatch(func(context.Context) error { return nil }))
	require.NoError(t, first.RecordProviderID("message-1"))
	ctx.AdvanceAttempt()
	decision := newFence(t, ctx).Decide()
	require.Equal(t, singledispatch.ReadBackRequired, decision.Kind)
	require.Equal(t, "message-1", decision.Marker.ProviderID)
}

func TestCanResendWaitsForSendOutcome(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	fence := newFence(t, ctx)
	require.Equal(t, singledispatch.SendAllowed, fence.Decide().Kind)
	require.NoError(t, fence.Dispatch(func(context.Context) error {
		require.ErrorIs(t, fence.RecordCanResend(), singledispatch.ErrDispatchNotAllowed)
		return nil
	}))
	require.NoError(t, fence.RecordCanResend())
}

func TestShortBudgetRetriesWithNotSent(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	fence, err := singledispatch.NewFence(sdkgo.Call{Context: ctx, ID: "call-1"}, 20*time.Millisecond)
	require.NoError(t, err)
	require.Equal(t, singledispatch.SendAllowed, fence.Decide().Kind)
	time.Sleep(10 * time.Millisecond)
	require.ErrorIs(t, fence.Dispatch(func(context.Context) error { t.Fatal("sent after write window"); return nil }), singledispatch.ErrInsufficientTime)
	ctx.AdvanceAttempt()
	require.Equal(t, singledispatch.SendAllowed, newFence(t, ctx).Decide().Kind)
}

func TestCheckpointWriteFailureNeverSends(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	fence := newFence(t, ctx)
	require.Equal(t, singledispatch.SendAllowed, fence.Decide().Kind)
	ctx.SetWriteError(errors.New("heartbeat unavailable"))
	require.ErrorIs(t, fence.Dispatch(func(context.Context) error { t.Fatal("sent after checkpoint failure"); return nil }), singledispatch.ErrCheckpoint)
	ctx.SetWriteError(nil)
	ctx.AdvanceAttempt()
	require.Equal(t, singledispatch.ReadBackRequired, newFence(t, ctx).Decide().Kind)
}

func TestLocalPhaseHeartbeatLossFailsClosed(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	ctx.DropLocalHeartbeats()
	ctx.SetLocalPhase(true)
	fence := newFence(t, ctx)
	require.Equal(t, singledispatch.SendAllowed, fence.Decide().Kind)
	require.NoError(t, fence.Dispatch(func(context.Context) error { return nil }))
	ctx.AdvanceAttempt()
	require.Equal(t, singledispatch.ReadBackRequired, newFence(t, ctx).Decide().Kind)
}
