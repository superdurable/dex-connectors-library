// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leadstatusdiscovery

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/copper/go"
	"github.com/superdurable/dex-connectors-library/connectors/copper/go/coppermock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestLeadStatusDiscoveryFlowIdentityAndMapping(t *testing.T) {
	input := copper.ListLeadStatusesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(copper.Connection{})))
	require.Equal(t, "StartListLeadStatuses", dex.GetFinalStepType[copper.ListLeadStatusesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := coppermock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
