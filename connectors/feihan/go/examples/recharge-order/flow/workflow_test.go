// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package orderstatus

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/feihan/go"
	"github.com/superdurable/dex-connectors-library/connectors/feihan/go/feihanmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestRechargeOrderFlowIdentityAndMapping(t *testing.T) {
	input := feihan.QueryRechargeOrderInput{MerchantOrderID: "ORDER123"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(feihan.Connection{})))
	require.Equal(t, "StartQueryRechargeOrder", dex.GetFinalStepType[feihan.QueryRechargeOrderInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := feihanmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
