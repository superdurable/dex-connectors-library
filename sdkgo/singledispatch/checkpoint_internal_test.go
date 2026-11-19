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

func TestFencesOfOneAttemptShareTheStepCheckpoint(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-share", "step-share")
	first, err := NewFence(sdkgo.Call{Context: ctx, ID: "call-a"}, time.Second)
	require.NoError(t, err)
	second, err := NewFence(sdkgo.Call{Context: ctx, ID: "call-b"}, time.Second)
	require.NoError(t, err)
	require.Same(t, first.checkpoint, second.checkpoint)
	registered, isRegistered := stepCheckpoints.Lookup(ctx)
	require.True(t, isRegistered)
	require.Same(t, first.checkpoint, registered)

	ctx.AdvanceAttempt()
	retried, err := NewFence(sdkgo.Call{Context: ctx, ID: "call-a"}, time.Second)
	require.NoError(t, err)
	require.NotSame(t, first.checkpoint, retried.checkpoint, "a later attempt starts a new checkpoint")
}
