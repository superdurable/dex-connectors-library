// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onlinestatus

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/xingyapi/go"
	"github.com/superdurable/dex-connectors-library/connectors/xingyapi/go/xingyapimock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetInstanceOnlineStatusFlowIdentityAndMapping(t *testing.T) {
	input := xingyapi.GetInstanceOnlineStatusInput{InstanceGUID: "inst_example123"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(xingyapi.Connection{})))
	require.Equal(t, "StartGetInstanceOnlineStatus", dex.GetFinalStepType[xingyapi.GetInstanceOnlineStatusInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := xingyapimock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
