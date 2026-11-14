// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package balanceflow

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/cloopen/go"
	"github.com/superdurable/dex-connectors-library/connectors/cloopen/go/cloopenmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestBalanceFlowIdentityAndMapping(t *testing.T) {
	input := cloopen.GetBalanceInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(cloopen.Connection{})))
	require.Equal(t, "StartGetBalance", dex.GetFinalStepType[cloopen.GetBalanceInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := cloopenmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
