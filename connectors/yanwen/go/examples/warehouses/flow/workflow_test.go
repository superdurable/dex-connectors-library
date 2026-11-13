// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package warehouseflow

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/yanwen/go"
	"github.com/superdurable/dex-connectors-library/connectors/yanwen/go/yanwenmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestWarehouseFlowFlowIdentityAndMapping(t *testing.T) {
	input := yanwen.ListWarehousesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(yanwen.Connection{})))
	require.Equal(t, "StartListWarehouses", dex.GetFinalStepType[yanwen.ListWarehousesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := yanwenmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
