// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package warehouselist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/yijingtong/go"
	"github.com/superdurable/dex-connectors-library/connectors/yijingtong/go/yijingtongmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestWarehouseListFlowIdentityAndMapping(t *testing.T) {
	input := yijingtong.ListWarehousesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(yijingtong.Connection{})))
	require.Equal(t, "StartListWarehouses", dex.GetFinalStepType[yijingtong.ListWarehousesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := yijingtongmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
