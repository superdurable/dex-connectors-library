// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package singledispatch

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/fenceobservation"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

// An agent host must read a resend after a possible earlier send as a possible dispatch.
func TestReadBackThenResendReportsPossibleDispatch(t *testing.T) {
	ctx := singledispatchtest.NewContext("flow-1", "step-1")
	ctx.AdvanceAttempt()
	require.NoError(t, ctx.SetPersistedCheckpoint(Marker{CallID: "call-1", Attempt: 1, State: StateClaimed, SendCount: 1}))
	observation, endObservation := fenceobservation.Begin(fenceobservation.AttemptIdentity{CallID: "call-1", Attempt: 2})
	defer endObservation()
	fence, err := NewFence(sdkgo.Call{Context: ctx, ID: "call-1"}, time.Second, WithMaxSends(3))
	require.NoError(t, err)
	require.Equal(t, ReadBackThenResendAllowed, fence.Decide().Kind)
	require.True(t, observation.MayHaveDispatched())
}
