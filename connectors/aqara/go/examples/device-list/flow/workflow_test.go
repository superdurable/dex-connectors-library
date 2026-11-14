// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package devicelist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/aqara/go"
	"github.com/superdurable/dex-connectors-library/connectors/aqara/go/aqaramock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestDevicesFlowIdentityAndMapping(t *testing.T) {
	input := aqara.ListDevicesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(aqara.Connection{})))
	require.Equal(t, "StartListDevices", dex.GetFinalStepType[aqara.ListDevicesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := aqaramock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
