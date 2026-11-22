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

const (
	priorSendsCallID      sdkgo.CallID = "prior-sends-call"
	priorSendsOtherCallID sdkgo.CallID = "prior-sends-other-call"
)

var errAnswerLost = errors.New("the provider's answer was lost")

// recordedAttempt runs one attempt of a fenced call, recording what a connector records.
type recordedAttempt func(t *testing.T, ctx *singledispatchtest.Context, callID sdkgo.CallID)

// newPriorSendsFence uses the highest cap so every history below can send again.
func newPriorSendsFence(t *testing.T, ctx *singledispatchtest.Context, callID sdkgo.CallID) *singledispatch.Fence {
	t.Helper()
	fence, err := singledispatch.NewFence(sdkgo.Call{Context: ctx, ID: callID}, time.Second, singledispatch.WithMaxSends(sdkgo.MaxSendsCeiling))
	require.NoError(t, err)
	return fence
}

func dispatchedFence(t *testing.T, ctx *singledispatchtest.Context, callID sdkgo.CallID, sendResult error) *singledispatch.Fence {
	t.Helper()
	fence := newPriorSendsFence(t, ctx, callID)
	kind := fence.Decide().Kind
	require.Contains(t, []singledispatch.DecisionKind{singledispatch.SendAllowed, singledispatch.ReadBackThenResendAllowed}, kind)
	require.ErrorIs(t, fence.Dispatch(func(context.Context) error { return sendResult }), sendResult)
	return fence
}

func claimWithLostAnswer(t *testing.T, ctx *singledispatchtest.Context, callID sdkgo.CallID) {
	t.Helper()
	dispatchedFence(t, ctx, callID, errAnswerLost)
}

func claimWithLostHeartbeat(t *testing.T, ctx *singledispatchtest.Context, callID sdkgo.CallID) {
	t.Helper()
	ctx.LoseNextHeartbeat()
	dispatchedFence(t, ctx, callID, errAnswerLost)
}

func sentWithProviderID(t *testing.T, ctx *singledispatchtest.Context, callID sdkgo.CallID) {
	t.Helper()
	require.NoError(t, dispatchedFence(t, ctx, callID, nil).RecordProviderID("object-1"))
}

func refusedWith429(t *testing.T, ctx *singledispatchtest.Context, callID sdkgo.CallID) {
	t.Helper()
	require.NoError(t, dispatchedFence(t, ctx, callID, nil).RecordCanResend())
}

func retriedBeforeSending(t *testing.T, ctx *singledispatchtest.Context, callID sdkgo.CallID) {
	t.Helper()
	fence := newPriorSendsFence(t, ctx, callID)
	require.Equal(t, singledispatch.SendAllowed, fence.Decide().Kind)
	require.NoError(t, fence.RecordNotSent())
}

// recordedNothing is an attempt that ended before Decide, such as a Worker crash.
func recordedNothing(*testing.T, *singledispatchtest.Context, sdkgo.CallID) {}

// requireReadEqualsDecide checks the read before Decide, as a wrapper calls it, and returns it.
func requireReadEqualsDecide(t *testing.T, ctx *singledispatchtest.Context, callID sdkgo.CallID) (int32, error) {
	t.Helper()
	heartbeatsBefore := len(ctx.Heartbeats())
	priorSends, readErr := singledispatch.MaximumPriorSends(ctx, callID)
	require.Len(t, ctx.Heartbeats(), heartbeatsBefore, "the read records no heartbeat")
	decision := newPriorSendsFence(t, ctx, callID).Decide()
	require.Equal(t, decision.MaximumPriorSends, priorSends)
	require.Equal(t, decision.Err, readErr)
	require.Equal(t, decision.Kind == singledispatch.FailClosed, readErr != nil)
	return priorSends, readErr
}

func TestMaximumPriorSendsEqualsDecideAfterRecordedAttempts(t *testing.T) {
	for _, scenario := range []struct {
		name           string
		earlier        []recordedAttempt
		wantPriorSends int32
	}{
		{name: "no checkpoint on attempt one", wantPriorSends: 0},
		{name: "no checkpoint on attempt two", earlier: []recordedAttempt{recordedNothing}, wantPriorSends: 1},
		{name: "no checkpoint on attempt three", earlier: []recordedAttempt{recordedNothing, recordedNothing}, wantPriorSends: 2},
		{name: "claim whose heartbeat was lost", earlier: []recordedAttempt{claimWithLostHeartbeat}, wantPriorSends: 1},
		{name: "claim with a lost answer", earlier: []recordedAttempt{claimWithLostAnswer}, wantPriorSends: 1},
		{name: "sent with a provider ID", earlier: []recordedAttempt{sentWithProviderID}, wantPriorSends: 1},
		{name: "refused with 429", earlier: []recordedAttempt{refusedWith429}, wantPriorSends: 0},
		{name: "refused twice", earlier: []recordedAttempt{refusedWith429, refusedWith429}, wantPriorSends: 0},
		{name: "pre-send Retry", earlier: []recordedAttempt{retriedBeforeSending}, wantPriorSends: 0},
		{name: "refused, then claimed", earlier: []recordedAttempt{refusedWith429, claimWithLostAnswer}, wantPriorSends: 1},
		{name: "claimed twice", earlier: []recordedAttempt{claimWithLostAnswer, claimWithLostAnswer}, wantPriorSends: 2},
		{name: "claim adds the attempts after it", earlier: []recordedAttempt{claimWithLostAnswer, recordedNothing, recordedNothing}, wantPriorSends: 3},
		{name: "refusal adds the attempts after it", earlier: []recordedAttempt{refusedWith429, recordedNothing}, wantPriorSends: 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := singledispatchtest.NewContext("flow-prior-sends", "step-prior-sends")
			for _, record := range scenario.earlier {
				record(t, ctx, priorSendsCallID)
				ctx.AdvanceAttempt()
			}
			priorSends, err := requireReadEqualsDecide(t, ctx, priorSendsCallID)
			require.NoError(t, err)
			require.Equal(t, scenario.wantPriorSends, priorSends)
		})
	}
}

func TestMaximumPriorSendsReadsEachCallIDOfBatchedMarkers(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-prior-sends", "step-prior-sends")
	claimWithLostAnswer(t, ctx, priorSendsCallID)
	refusedWith429(t, ctx, priorSendsOtherCallID)
	ctx.AdvanceAttempt()

	claimed, err := requireReadEqualsDecide(t, ctx, priorSendsCallID)
	require.NoError(t, err)
	require.Equal(t, int32(1), claimed)
	refused, err := requireReadEqualsDecide(t, ctx, priorSendsOtherCallID)
	require.NoError(t, err)
	require.Equal(t, int32(0), refused)
	missing, err := requireReadEqualsDecide(t, ctx, "prior-sends-unrecorded-call")
	require.NoError(t, err)
	require.Equal(t, int32(1), missing, "a Call ID without an entry assumes each earlier attempt sent")
}

func TestMaximumPriorSendsEqualsDecideForLegacyAndInvalidCheckpoints(t *testing.T) {
	for _, scenario := range []struct {
		name           string
		attempt        int32
		checkpoint     any
		wantPriorSends int32
		wantErr        bool
	}{
		{name: "legacy claim counts its attempt", attempt: 2, checkpoint: singledispatch.Marker{CallID: priorSendsCallID, Attempt: 1, State: singledispatch.StateClaimed}, wantPriorSends: 1},
		{name: "legacy claim with a count", attempt: 4, checkpoint: singledispatch.Marker{CallID: priorSendsCallID, Attempt: 3, State: singledispatch.StateClaimed, SendCount: 2}, wantPriorSends: 2},
		{name: "legacy canResend counts the attempts before it", attempt: 2, checkpoint: singledispatch.Marker{CallID: priorSendsCallID, Attempt: 1, State: singledispatch.StateCanResend}, wantPriorSends: 0},
		{name: "legacy stale notSent adds the attempts after it", attempt: 4, checkpoint: singledispatch.Marker{CallID: priorSendsCallID, Attempt: 2, State: singledispatch.StateNotSent}, wantPriorSends: 2},
		{name: "legacy marker of another Call ID", attempt: 3, checkpoint: singledispatch.Marker{CallID: priorSendsOtherCallID, Attempt: 2, State: singledispatch.StateNotSent}, wantPriorSends: 2},
		{name: "unreadable value", attempt: 2, checkpoint: "not a checkpoint", wantPriorSends: 1},
		{name: "claim with an impossible count", attempt: 2, checkpoint: map[string]any{"singleDispatchMarkers": map[string]any{
			string(priorSendsCallID): map[string]any{"callId": string(priorSendsCallID), "attempt": 1, "state": "claimed", "sendCount": 0},
		}}, wantErr: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := contextAtAttempt(t, scenario.attempt, nil)
			require.NoError(t, ctx.SetPersistedCheckpoint(scenario.checkpoint))
			priorSends, err := requireReadEqualsDecide(t, ctx, priorSendsCallID)
			require.Equal(t, scenario.wantErr, err != nil)
			require.Equal(t, scenario.wantPriorSends, priorSends)
		})
	}
}

func TestMaximumPriorSendsReturnsTheHeartbeatReadErrorOfDecide(t *testing.T) {
	ctx := contextAtAttempt(t, 2, nil)
	readErr := errors.New("heartbeat unavailable")
	ctx.SetReadError(readErr)
	priorSends, err := requireReadEqualsDecide(t, ctx, priorSendsCallID)
	require.ErrorIs(t, err, readErr)
	require.Zero(t, priorSends)
}

func TestMaximumPriorSendsCountsOnlyEarlierAttempts(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-prior-sends", "step-prior-sends")
	claimWithLostAnswer(t, ctx, priorSendsCallID)
	priorSends, err := singledispatch.MaximumPriorSends(ctx, priorSendsCallID)
	require.NoError(t, err)
	require.Zero(t, priorSends, "this attempt's own send is not an earlier send")
}

func TestMaximumPriorSendsRejectsAMissingContextOrCallID(t *testing.T) {
	_, err := singledispatch.MaximumPriorSends(nil, priorSendsCallID)
	require.Error(t, err)
	_, err = singledispatch.MaximumPriorSends(singledispatchtest.NewContext("flow-prior-sends", "step-prior-sends"), "")
	require.Error(t, err)
}
