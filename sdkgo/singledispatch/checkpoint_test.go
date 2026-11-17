// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package singledispatch_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func newCallFence(t *testing.T, ctx *singledispatchtest.Context, callID sdkgo.CallID, maxSends int32) *singledispatch.Fence {
	t.Helper()
	fence, err := singledispatch.NewFence(sdkgo.Call{Context: ctx, ID: callID}, time.Second, singledispatch.WithMaxSends(maxSends))
	require.NoError(t, err)
	return fence
}

func lastMarkers(t *testing.T, ctx *singledispatchtest.Context) map[sdkgo.CallID]singledispatch.Marker {
	t.Helper()
	heartbeats := ctx.Heartbeats()
	require.NotEmpty(t, heartbeats)
	var checkpoint struct {
		SingleDispatchMarkers map[sdkgo.CallID]singledispatch.Marker `json:"singleDispatchMarkers"`
	}
	require.NoError(t, json.Unmarshal(heartbeats[len(heartbeats)-1], &checkpoint))
	return checkpoint.SingleDispatchMarkers
}

func TestTwoFencedCallsInOneStepBothSend(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	sends := map[sdkgo.CallID]int{}
	for _, callID := range []sdkgo.CallID{"call-gmail", "call-slack"} {
		fence := newCallFence(t, ctx, callID, 1)
		require.Equal(t, singledispatch.SendAllowed, fence.Decide().Kind, callID)
		require.NoError(t, fence.Dispatch(func(context.Context) error { sends[callID]++; return nil }))
	}
	require.Equal(t, map[sdkgo.CallID]int{"call-gmail": 1, "call-slack": 1}, sends)
	require.Equal(t, map[sdkgo.CallID]singledispatch.Marker{
		"call-gmail": {CallID: "call-gmail", Attempt: 1, State: singledispatch.StateClaimed},
		"call-slack": {CallID: "call-slack", Attempt: 1, State: singledispatch.StateClaimed},
	}, lastMarkers(t, ctx))
}

func TestLaterCallKeepsEarlierCallsPreviousAttemptEntry(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	first := newCallFence(t, ctx, "call-a", 1)
	require.Equal(t, singledispatch.SendAllowed, first.Decide().Kind)
	require.NoError(t, first.Dispatch(func(context.Context) error { return nil }))
	second := newCallFence(t, ctx, "call-b", 1)
	require.Equal(t, singledispatch.SendAllowed, second.Decide().Kind)
	require.NoError(t, second.Dispatch(func(context.Context) error { return nil }))
	require.NoError(t, second.RecordCanResend())

	ctx.AdvanceAttempt()
	retriedB := newCallFence(t, ctx, "call-b", 1)
	require.Equal(t, singledispatch.SendAllowed, retriedB.Decide().Kind)
	require.NoError(t, retriedB.Dispatch(func(context.Context) error { return nil }))
	require.Equal(t, singledispatch.Marker{CallID: "call-a", Attempt: 1, State: singledispatch.StateClaimed}, lastMarkers(t, ctx)["call-a"])

	ctx.AdvanceAttempt()
	retriedA := newCallFence(t, ctx, "call-a", 1)
	decision := retriedA.Decide()
	require.Equal(t, singledispatch.ReadBackRequired, decision.Kind, "call A's claim survived call B's write")
	require.Equal(t, int32(1), decision.Marker.Attempt)
}

func TestConcurrentFencesInOneStepKeepEveryEntry(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	const callCount = 16
	var group sync.WaitGroup
	for index := range callCount {
		group.Add(1)
		go func() {
			defer group.Done()
			callID := sdkgo.CallID(fmt.Sprintf("call-%02d", index))
			fence, err := singledispatch.NewFence(sdkgo.Call{Context: ctx, ID: callID}, time.Second, singledispatch.WithMaxSends(3))
			if err != nil {
				panic(err)
			}
			if fence.Decide().Kind != singledispatch.SendAllowed {
				panic("concurrent fence was not allowed to send")
			}
			if err := fence.Dispatch(func(context.Context) error { return nil }); err != nil {
				panic(err)
			}
			if err := fence.RecordProviderID("object-" + string(callID)); err != nil {
				panic(err)
			}
		}()
	}
	group.Wait()
	markers := lastMarkers(t, ctx)
	require.Len(t, markers, callCount)
	for callID, marker := range markers {
		require.Equal(t, singledispatch.Marker{CallID: callID, Attempt: 1, State: singledispatch.StateClaimed, ProviderID: "object-" + string(callID), SendCount: 1}, marker)
	}

	ctx.AdvanceAttempt()
	for index := range callCount {
		decision := newCallFence(t, ctx, sdkgo.CallID(fmt.Sprintf("call-%02d", index)), 3).Decide()
		require.Equal(t, singledispatch.ReadBackThenResendAllowed, decision.Kind)
		require.Equal(t, int32(1), decision.MaximumPriorSends)
	}
}

func TestFailedHeartbeatDoesNotCommitEntry(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	failing := newCallFence(t, ctx, "call-a", 1)
	require.Equal(t, singledispatch.SendAllowed, failing.Decide().Kind)
	ctx.SetWriteError(fmt.Errorf("heartbeat unavailable"))
	require.ErrorIs(t, failing.Dispatch(func(context.Context) error { t.Fatal("sent after failed claim"); return nil }), singledispatch.ErrCheckpoint)
	ctx.SetWriteError(nil)
	other := newCallFence(t, ctx, "call-b", 1)
	require.Equal(t, singledispatch.SendAllowed, other.Decide().Kind)
	require.NoError(t, other.Dispatch(func(context.Context) error { return nil }))
	require.Equal(t, []sdkgo.CallID{"call-b"}, keys(lastMarkers(t, ctx)))
}

func keys(markers map[sdkgo.CallID]singledispatch.Marker) []sdkgo.CallID {
	callIDs := make([]sdkgo.CallID, 0, len(markers))
	for callID := range markers {
		callIDs = append(callIDs, callID)
	}
	return callIDs
}
