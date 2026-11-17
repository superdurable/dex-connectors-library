// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package singledispatch

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func registeredStepCheckpoint(key any) (*stepCheckpoint, bool) {
	stepCheckpoints.mu.Lock()
	defer stepCheckpoints.mu.Unlock()
	checkpoint, isRegistered := stepCheckpoints.byContext[key]
	return checkpoint, isRegistered
}

func TestStepCheckpointIsReleasedWhenAttemptContextEnds(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-release", "step-release")
	cancel := ctx.SetDeadline(time.Now().Add(time.Minute))
	first, err := NewFence(sdkgo.Call{Context: ctx, ID: "call-a"}, time.Second)
	require.NoError(t, err)
	second, err := NewFence(sdkgo.Call{Context: ctx, ID: "call-b"}, time.Second)
	require.NoError(t, err)
	require.Same(t, first.checkpoint, second.checkpoint, "Fences of one Step attempt share a checkpoint")
	checkpoint, isRegistered := registeredStepCheckpoint(ctx)
	require.True(t, isRegistered)
	require.Same(t, first.checkpoint, checkpoint)
	cancel()
	require.Eventually(t, func() bool {
		_, isRegistered := registeredStepCheckpoint(ctx)
		return !isRegistered
	}, time.Second, time.Millisecond)
}

func TestStepCheckpointIsReplacedByLaterAttempt(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-replace", "step-replace")
	first, err := NewFence(sdkgo.Call{Context: ctx, ID: "call-a"}, time.Second)
	require.NoError(t, err)
	ctx.AdvanceAttempt()
	second, err := NewFence(sdkgo.Call{Context: ctx, ID: "call-a"}, time.Second)
	require.NoError(t, err)
	require.NotSame(t, first.checkpoint, second.checkpoint)
	checkpoint, isRegistered := registeredStepCheckpoint(ctx)
	require.True(t, isRegistered)
	require.Same(t, second.checkpoint, checkpoint)
	releaseStepCheckpoint(ctx, second.checkpoint)
}
