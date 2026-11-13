// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package orderstatus

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/dada/go"
	"github.com/superdurable/dex-connectors-library/connectors/dada/go/dadamock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestOrderStatusFlowIdentityAndMapping(t *testing.T) {
	input := dada.QueryOrderStatusInput{OrderID: "orderNo1653708880866"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(dada.Connection{})))
	require.Equal(t, "StartQueryOrderStatus", dex.GetFinalStepType[dada.QueryOrderStatusInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := dadamock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
