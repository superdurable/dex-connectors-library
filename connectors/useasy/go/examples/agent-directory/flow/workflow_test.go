// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package agentcatalog

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/useasy/go"
	"github.com/superdurable/dex-connectors-library/connectors/useasy/go/useasymock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestAgentCatalogFlowIdentityAndMapping(t *testing.T) {
	input := useasy.ListAgentsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(useasy.Connection{})))
	require.Equal(t, "StartListAgents", dex.GetFinalStepType[useasy.ListAgentsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := useasymock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
