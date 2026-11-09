// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clientdiscovery

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/campaignmonitor/go"
	"github.com/superdurable/dex-connectors-library/connectors/campaignmonitor/go/campaignmonitormock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestClientDiscoveryFlowIdentityAndMapping(t *testing.T) {
	input := campaignmonitor.ListClientsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(campaignmonitor.Connection{})))
	require.Equal(t, "StartListClients", dex.GetFinalStepType[campaignmonitor.ListClientsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := campaignmonitormock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
