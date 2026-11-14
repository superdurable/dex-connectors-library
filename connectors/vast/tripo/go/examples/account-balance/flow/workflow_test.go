// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountbalance

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/vast/tripo/go"
	"github.com/superdurable/dex-connectors-library/connectors/vast/tripo/go/tripomock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestAccountBalanceFlowIdentityAndMapping(t *testing.T) {
	input := tripo.GetAccountBalanceInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(tripo.Connection{})))
	require.Equal(t, "StartGetAccountBalance", dex.GetFinalStepType[tripo.GetAccountBalanceInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := tripomock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
