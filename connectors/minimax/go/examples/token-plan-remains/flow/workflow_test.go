// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tokenplanremains

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/minimax/go"
	"github.com/superdurable/dex-connectors-library/connectors/minimax/go/minimaxmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTokenPlanRemainsFlowIdentityAndMapping(t *testing.T) {
	input := minimax.GetTokenPlanRemainsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(minimax.Connection{})))
	require.Equal(t, "StartGetTokenPlanRemains", dex.GetFinalStepType[minimax.GetTokenPlanRemainsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := minimaxmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
