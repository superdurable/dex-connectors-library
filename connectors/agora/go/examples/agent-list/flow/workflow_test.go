// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package agentlist

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/agora/go"
	"github.com/superdurable/dex-connectors-library/connectors/agora/go/agoramock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestAgentListFlowIdentityAndMapping(t *testing.T) {
	input := agora.ListRunningAgentsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(agora.Connection{})))
	require.Equal(t, "StartListRunningAgents", dex.GetFinalStepType[agora.ListRunningAgentsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := agoramock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
