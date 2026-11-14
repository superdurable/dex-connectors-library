// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package waybilltrace

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/cainiao/go"
	"github.com/superdurable/dex-connectors-library/connectors/cainiao/go/cainiaomock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestWaybillTraceFlowIdentityAndMapping(t *testing.T) {
	input := cainiao.QueryWaybillTraceInput{CarrierCode: "YTO", WaybillCode: "12345"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(cainiao.Connection{})))
	require.Equal(t, "StartQueryWaybillTrace", dex.GetFinalStepType[cainiao.QueryWaybillTraceInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := cainiaomock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
