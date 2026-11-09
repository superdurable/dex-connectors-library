// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leaddiscovery

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/sell/go"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/sell/go/sellmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestLeadDiscoveryFlowIdentityAndMapping(t *testing.T) {
	input := sell.ListLeadsInput{PerPage: 20, Page: 1}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(sell.Connection{})))
	require.Equal(t, "StartListLeads", dex.GetFinalStepType[sell.ListLeadsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := sellmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
