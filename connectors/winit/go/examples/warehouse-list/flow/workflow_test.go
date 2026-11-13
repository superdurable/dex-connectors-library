// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package warehouselist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/winit/go"
	"github.com/superdurable/dex-connectors-library/connectors/winit/go/winitmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestWarehouseListFlowIdentityAndMapping(t *testing.T) {
	input := winit.ListWarehousesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(winit.Connection{})))
	require.Equal(t, "StartListWarehouses", dex.GetFinalStepType[winit.ListWarehousesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := winitmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
