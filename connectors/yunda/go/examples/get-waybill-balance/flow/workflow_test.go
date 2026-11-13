// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package waybillbalance

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/yunda/go"
	"github.com/superdurable/dex-connectors-library/connectors/yunda/go/yundamock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetWaybillBalanceFlowIdentityAndMapping(t *testing.T) {
	input := yunda.GetWaybillBalanceInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(yunda.Connection{})))
	require.Equal(t, "StartGetWaybillBalance", dex.GetFinalStepType[yunda.GetWaybillBalanceInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := yundamock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
