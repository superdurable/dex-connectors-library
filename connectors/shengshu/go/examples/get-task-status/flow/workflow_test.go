// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taskstatus

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/shengshu/go"
	"github.com/superdurable/dex-connectors-library/connectors/shengshu/go/shengshumock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetTaskStatusFlowIdentityAndMapping(t *testing.T) {
	input := shengshu.GetTaskStatusInput{TaskID: "task-123"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(shengshu.Connection{})))
	require.Equal(t, "StartGetTaskStatus", dex.GetFinalStepType[shengshu.GetTaskStatusInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := shengshumock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
