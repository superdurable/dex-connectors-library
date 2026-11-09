// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taskinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/teamwork/go"
	"github.com/superdurable/dex-connectors-library/connectors/teamwork/go/teamworkmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTaskInspectionFlowIdentityAndMapping(t *testing.T) {
	input := teamwork.GetTaskInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(teamwork.Connection{})))
	require.Equal(t, "StartGetTask", dex.GetFinalStepType[teamwork.GetTaskInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := teamworkmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
