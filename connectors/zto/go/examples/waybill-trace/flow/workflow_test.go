// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package waybilltrace

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zto/go"
	"github.com/superdurable/dex-connectors-library/connectors/zto/go/ztomock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestWaybillTraceFlowIdentityAndMapping(t *testing.T) {
	input := zto.QueryWaybillTraceInput{BillCode: "73100059800035"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(zto.Connection{})))
	require.Equal(t, "StartQueryWaybillTrace", dex.GetFinalStepType[zto.QueryWaybillTraceInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := ztomock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
