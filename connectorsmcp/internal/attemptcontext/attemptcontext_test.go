// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package attemptcontext

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type checkpoint struct {
	State   string `json:"state"`
	Attempt int32  `json:"attempt"`
	Payload []byte `json:"payload"`
}

func TestContextReportsTheOptionsIdentity(t *testing.T) {
	firstAttemptAt := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	ctx, err := New(t.Context(), Options{FlowID: "flow", StepExecutionID: "key", FirstAttemptAt: firstAttemptAt, Attempt: 2})
	require.NoError(t, err)
	require.Equal(t, "flow", ctx.FlowID())
	require.Equal(t, "flow", ctx.RunID())
	require.Equal(t, "key", ctx.StepExecutionID())
	require.Equal(t, firstAttemptAt, ctx.FirstAttemptAt())
	require.Equal(t, firstAttemptAt, ctx.FlowStartedAt())
	require.Equal(t, int32(2), ctx.Attempt())
	require.Empty(t, ctx.FromStepExecutionID())
	require.Nil(t, ctx.RecoveryError())
	require.False(t, ctx.HasTimerFired())
	require.False(t, ctx.HasTimerFiredByIndex(0))
	require.False(t, ctx.WaitForMethodFailed())

	_, err = New(t.Context(), Options{Attempt: 0})
	require.Error(t, err)
	_, err = New(t.Context(), Options{Attempt: 1, LastHeartbeat: json.RawMessage(`{`)})
	require.Error(t, err)
}

func TestHeartbeatsReachTheHookAndOnlyTheNextAttempt(t *testing.T) {
	var recorded []json.RawMessage
	first, err := New(t.Context(), Options{Attempt: 1, OnRecordHeartbeat: func(value json.RawMessage) error {
		recorded = append(recorded, value)
		return nil
	}})
	require.NoError(t, err)
	value := checkpoint{State: "claimed", Attempt: 1, Payload: []byte{0, 1, 2}}
	require.NoError(t, first.RecordHeartbeat(value))
	var typedNil *checkpoint
	require.NoError(t, first.RecordHeartbeat(typedNil))
	require.Len(t, recorded, 2)
	require.Nil(t, recorded[1], "a typed nil clears the checkpoint")
	var none checkpoint
	isFound, err := first.GetLastHeartbeatValue(&none)
	require.NoError(t, err)
	require.False(t, isFound)

	second, err := New(t.Context(), Options{Attempt: 2, LastHeartbeat: recorded[0]})
	require.NoError(t, err)
	var decoded checkpoint
	isFound, err = second.GetLastHeartbeatValue(&decoded)
	require.NoError(t, err)
	require.True(t, isFound)
	require.Equal(t, value, decoded)
	_, err = second.GetLastHeartbeatValue(decoded)
	require.Error(t, err, "the target must be a pointer")
}

func TestHooksNeverRunAfterTheAttemptEnded(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())
	calls := 0
	ctx, err := New(parent, Options{Attempt: 1, OnRecordHeartbeat: func(json.RawMessage) error { calls++; return nil }})
	require.NoError(t, err)
	cancel()
	require.ErrorIs(t, ctx.RecordHeartbeat(checkpoint{State: "claimed"}), context.Canceled)
	require.ErrorIs(t, ctx.SetStepExecutionLocal("cursor", 1), context.Canceled)
	require.ErrorIs(t, ctx.RecordEvent("sent", 1), context.Canceled)
	require.Zero(t, calls)
}

func TestLocalsAndEventsAreValidatedAndNotKept(t *testing.T) {
	ctx, err := New(t.Context(), Options{Attempt: 1})
	require.NoError(t, err)
	require.NoError(t, ctx.SetStepExecutionLocal("cursor", 5))
	var cursor int
	isFound, err := ctx.GetStepExecutionLocal("cursor", &cursor)
	require.NoError(t, err)
	require.False(t, isFound, "as for dex.NewStandaloneContext, locals reach no attempt")
	require.Error(t, ctx.SetStepExecutionLocal("", 1))
	require.NoError(t, ctx.RecordEvent("sent", map[string]int{"count": 1}))
	require.Error(t, ctx.RecordEvent("sent", 1), "an event name is recorded once")
	require.Error(t, ctx.RecordEvent("", 1))
	require.Error(t, ctx.RecordHeartbeat(func() {}), "an unencodable heartbeat is refused")
}
