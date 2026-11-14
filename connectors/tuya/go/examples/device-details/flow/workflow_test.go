// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package devicedetails

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/tuya/go"
	"github.com/superdurable/dex-connectors-library/connectors/tuya/go/tuyamock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestDeviceDetailsFlowIdentityAndMapping(t *testing.T) {
	input := tuya.GetDeviceDetailsInput{DeviceID: "60613135b121cddc294a"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(tuya.Connection{})))
	require.Equal(t, "StartGetDeviceDetails", dex.GetFinalStepType[tuya.GetDeviceDetailsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := tuyamock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
