// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectdiscovery

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/azure-devops/go"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/azure-devops/go/azuredevopsmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestProjectDiscoveryFlowIdentityAndMapping(t *testing.T) {
	input := azuredevops.ListProjectsInput{Top: 20, Skip: 0}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(azuredevops.Connection{})))
	require.Equal(t, "StartListProjects", dex.GetFinalStepType[azuredevops.ListProjectsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := azuredevopsmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
