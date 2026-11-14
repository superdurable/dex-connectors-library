// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dashboardspage

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/guance/go"
	"github.com/superdurable/dex-connectors-library/connectors/guance/go/guancemock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListDashboardsPageFlowIdentityAndMapping(t *testing.T) {
	input := guance.ListDashboardsPageInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(guance.Connection{})))
	require.Equal(t, "StartListDashboardsPage", dex.GetFinalStepType[guance.ListDashboardsPageInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := guancemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
