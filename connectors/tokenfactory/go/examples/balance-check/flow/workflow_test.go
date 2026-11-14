// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package balancecheck

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/tokenfactory/go"
	"github.com/superdurable/dex-connectors-library/connectors/tokenfactory/go/tokenfactorymock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestBalanceFlowIdentityAndMapping(t *testing.T) {
	input := tokenfactory.GetBalanceInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(tokenfactory.Connection{})))
	require.Equal(t, "StartGetBalance", dex.GetFinalStepType[tokenfactory.GetBalanceInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := tokenfactorymock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
