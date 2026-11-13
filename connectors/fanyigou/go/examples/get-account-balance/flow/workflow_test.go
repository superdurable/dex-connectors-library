// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountBalance

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/fanyigou/go"
	"github.com/superdurable/dex-connectors-library/connectors/fanyigou/go/fanyigoumock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetAccountBalanceFlowIdentityAndMapping(t *testing.T) {
	input := fanyigou.GetAccountBalanceInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(fanyigou.Connection{})))
	require.Equal(t, "StartGetAccountBalance", dex.GetFinalStepType[fanyigou.GetAccountBalanceInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := fanyigoumock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
