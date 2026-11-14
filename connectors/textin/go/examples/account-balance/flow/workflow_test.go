// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountbalance

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/textin/go"
	"github.com/superdurable/dex-connectors-library/connectors/textin/go/textinmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestAccountBalanceFlowIdentityAndMapping(t *testing.T) {
	input := textin.GetAccountBalanceInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(textin.Connection{})))
	require.Equal(t, "StartGetAccountBalance", dex.GetFinalStepType[textin.GetAccountBalanceInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := textinmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
