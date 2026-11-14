// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package balanceread

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/yunpian/go"
	"github.com/superdurable/dex-connectors-library/connectors/yunpian/go/yunpianmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestBalanceReadFlowIdentityAndMapping(t *testing.T) {
	input := yunpian.GetAccountBalanceInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(yunpian.Connection{})))
	require.Equal(t, "StartGetAccountBalance", dex.GetFinalStepType[yunpian.GetAccountBalanceInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := yunpianmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
