// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package opportunityinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/lever/go"
	"github.com/superdurable/dex-connectors-library/connectors/lever/go/levermock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestOpportunityInspectionFlowIdentityAndMapping(t *testing.T) {
	input := lever.GetOpportunityInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(lever.Connection{})))
	require.Equal(t, "StartGetOpportunity", dex.GetFinalStepType[lever.GetOpportunityInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := levermock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
