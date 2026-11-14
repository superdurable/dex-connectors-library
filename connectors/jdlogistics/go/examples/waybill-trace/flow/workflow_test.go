// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package waybilltrace

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/jdlogistics/go"
	"github.com/superdurable/dex-connectors-library/connectors/jdlogistics/go/jdlogisticsmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestWaybillTraceFlowIdentityAndMapping(t *testing.T) {
	input := jdlogistics.QueryWaybillTraceInput{WaybillNo: "JDX009866825381"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(jdlogistics.Connection{})))
	require.Equal(t, "StartQueryWaybillTrace", dex.GetFinalStepType[jdlogistics.QueryWaybillTraceInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := jdlogisticsmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
