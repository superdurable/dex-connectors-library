// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package campaigndiscovery

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/tiktok/go"
	"github.com/superdurable/dex-connectors-library/connectors/tiktok/go/tiktokmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestCampaignDiscoveryFlowIdentityAndMapping(t *testing.T) {
	input := tiktok.ListCampaignsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(tiktok.Connection{})))
	require.Equal(t, "StartListCampaigns", dex.GetFinalStepType[tiktok.ListCampaignsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := tiktokmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
