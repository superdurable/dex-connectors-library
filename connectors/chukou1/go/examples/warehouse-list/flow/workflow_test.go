// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package warehouselist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/chukou1/go"
	"github.com/superdurable/dex-connectors-library/connectors/chukou1/go/chukou1mock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestWarehouseListFlowIdentityAndMapping(t *testing.T) {
	input := chukou1.ListWarehousesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(chukou1.Connection{})))
	require.Equal(t, "StartListWarehouses", dex.GetFinalStepType[chukou1.ListWarehousesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := chukou1mock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
