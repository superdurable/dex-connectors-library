// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package balanceflow

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/chuanglan/go"
	"github.com/superdurable/dex-connectors-library/connectors/chuanglan/go/chuanglanmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestBalanceFlowIdentityAndMapping(t *testing.T) {
	input := chuanglan.GetBalanceInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(chuanglan.Connection{})))
	require.Equal(t, "StartGetBalance", dex.GetFinalStepType[chuanglan.GetBalanceInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := chuanglanmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
