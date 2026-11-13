// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package chargestatusflow

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/pingpp/go"
	"github.com/superdurable/dex-connectors-library/connectors/pingpp/go/pingppmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestChargeStatusFlowIdentityAndMapping(t *testing.T) {
	input := pingpp.GetChargeStatusInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(pingpp.Connection{})))
	require.Equal(t, "StartGetChargeStatus", dex.GetFinalStepType[pingpp.GetChargeStatusInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := pingppmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
