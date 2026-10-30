// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package demo

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/alphavantage/go"
	"github.com/superdurable/dex-connectors-library/connectors/alphavantage/go/alphavantagemock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestDemoFlowIdentityAndMapping(t *testing.T) {
	input := alphavantage.StockInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(alphavantage.Connection{})))
	require.Equal(t, "StartGetGlobalQuote", dex.GetFinalStepType[alphavantage.StockInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := alphavantagemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
