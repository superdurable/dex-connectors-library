// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package vehiclebrands

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/wapi/go"
	"github.com/superdurable/dex-connectors-library/connectors/wapi/go/wapimock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListVehicleBrandsFlowIdentityAndMapping(t *testing.T) {
	input := wapi.ListVehicleBrandsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(wapi.Connection{})))
	require.Equal(t, "StartListVehicleBrands", dex.GetFinalStepType[wapi.ListVehicleBrandsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := wapimock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
