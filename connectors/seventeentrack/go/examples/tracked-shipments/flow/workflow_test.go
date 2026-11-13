// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shipmentlist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/seventeentrack/go"
	"github.com/superdurable/dex-connectors-library/connectors/seventeentrack/go/seventeentrackmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestTrackedShipmentListFlowIdentityAndMapping(t *testing.T) {
	input := seventeentrack.ListTrackedShipmentsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(seventeentrack.Connection{})))
	require.Equal(t, "StartListTrackedShipments", dex.GetFinalStepType[seventeentrack.ListTrackedShipmentsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := seventeentrackmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
