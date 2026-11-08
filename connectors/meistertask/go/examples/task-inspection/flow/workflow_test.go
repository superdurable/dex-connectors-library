// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taskinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/meistertask/go"
	"github.com/superdurable/dex-connectors-library/connectors/meistertask/go/meistertaskmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTaskInspectionFlowIdentityAndMapping(t *testing.T) {
	input := meistertask.GetTaskInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(meistertask.Connection{})))
	require.Equal(t, "StartGetTask", dex.GetFinalStepType[meistertask.GetTaskInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := meistertaskmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
