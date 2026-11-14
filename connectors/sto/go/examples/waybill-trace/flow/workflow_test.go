// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package waybilltrace

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/sto/go"
	"github.com/superdurable/dex-connectors-library/connectors/sto/go/stomock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestWaybillTraceFlowIdentityAndMapping(t *testing.T) {
	input := sto.QueryWaybillTraceInput{WaybillNo: "884000400421364"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(sto.Connection{})))
	require.Equal(t, "StartQueryWaybillTrace", dex.GetFinalStepType[sto.QueryWaybillTraceInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := stomock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
