// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tradingday

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/tushare/go"
	"github.com/superdurable/dex-connectors-library/connectors/tushare/go/tusharemock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetTradingDayFlowIdentityAndMapping(t *testing.T) {
	input := tushare.GetTradingDayInput{Exchange: "SSE", Date: "20261113"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(tushare.Connection{})))
	require.Equal(t, "StartGetTradingDay", dex.GetFinalStepType[tushare.GetTradingDayInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := tusharemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
