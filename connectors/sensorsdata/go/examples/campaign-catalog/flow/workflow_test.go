// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package campaigncatalog

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/sensorsdata/go"
	"github.com/superdurable/dex-connectors-library/connectors/sensorsdata/go/sensorsdatamock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestCampaignCatalogFlowIdentityAndMapping(t *testing.T) {
	input := sensorsdata.ListCampaignsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(sensorsdata.Connection{})))
	require.Equal(t, "StartListCampaigns", dex.GetFinalStepType[sensorsdata.ListCampaignsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := sensorsdatamock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
