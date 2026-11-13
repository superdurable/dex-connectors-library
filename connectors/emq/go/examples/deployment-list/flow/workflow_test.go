// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package deploymentlist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/emq/go"
	"github.com/superdurable/dex-connectors-library/connectors/emq/go/emqmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestDeploymentListFlowIdentityAndMapping(t *testing.T) {
	input := emq.ListDeploymentsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(emq.Connection{})))
	require.Equal(t, "StartListDeployments", dex.GetFinalStepType[emq.ListDeploymentsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := emqmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
