// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package deviceread

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/onenet/go"
	"github.com/superdurable/dex-connectors-library/connectors/onenet/go/onenetmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestDeviceReadFlowIdentityAndMapping(t *testing.T) {
	input := onenet.QueryDeviceDetailInput{DeviceName: "sensor-1"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(onenet.Connection{})))
	require.Equal(t, "StartQueryDeviceDetail", dex.GetFinalStepType[onenet.QueryDeviceDetailInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := onenetmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
