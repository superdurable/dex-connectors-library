// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package parceltracking

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/kuaidibird/go"
	"github.com/superdurable/dex-connectors-library/connectors/kuaidibird/go/kuaidibirdmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestTrackingFlowIdentityAndMapping(t *testing.T) {
	input := kuaidibird.QueryTrackingInput{CarrierCode: "YTO", TrackingNumber: "YT1234567890123"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(kuaidibird.Connection{})))
	require.Equal(t, "StartQueryTracking", dex.GetFinalStepType[kuaidibird.QueryTrackingInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := kuaidibirdmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
