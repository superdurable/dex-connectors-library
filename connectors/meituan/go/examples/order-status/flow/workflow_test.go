// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package orderstatus

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/meituan/go"
	"github.com/superdurable/dex-connectors-library/connectors/meituan/go/meituanmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestOrderStatusFlowIdentityAndMapping(t *testing.T) {
	input := meituan.QueryOrderStatusInput{DeliveryID: 123456, MeituanOrderID: "MT123456789"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(meituan.Connection{})))
	require.Equal(t, "StartQueryOrderStatus", dex.GetFinalStepType[meituan.QueryOrderStatusInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := meituanmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
