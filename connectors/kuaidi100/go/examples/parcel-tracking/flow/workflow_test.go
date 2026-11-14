// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package parceltracking

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/kuaidi100/go"
	"github.com/superdurable/dex-connectors-library/connectors/kuaidi100/go/kuaidi100mock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestTrackingFlowIdentityAndMapping(t *testing.T) {
	input := kuaidi100.QueryTrackingInput{CarrierCode: "yuantong", TrackingNumber: "YT1234567890123"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(kuaidi100.Connection{})))
	require.Equal(t, "StartQueryTracking", dex.GetFinalStepType[kuaidi100.QueryTrackingInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := kuaidi100mock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
