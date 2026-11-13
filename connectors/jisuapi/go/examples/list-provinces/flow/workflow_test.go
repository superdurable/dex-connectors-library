// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package provinceslist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/jisuapi/go"
	"github.com/superdurable/dex-connectors-library/connectors/jisuapi/go/jisuapimock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListProvincesFlowIdentityAndMapping(t *testing.T) {
	input := jisuapi.ListProvincesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(jisuapi.Connection{})))
	require.Equal(t, "StartListProvinces", dex.GetFinalStepType[jisuapi.ListProvincesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := jisuapimock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
