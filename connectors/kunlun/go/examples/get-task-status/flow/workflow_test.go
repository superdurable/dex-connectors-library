// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taskstatus

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/kunlun/go"
	"github.com/superdurable/dex-connectors-library/connectors/kunlun/go/kunlunmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetTaskStatusFlowIdentityAndMapping(t *testing.T) {
	input := kunlun.GetTaskStatusInput{TaskID: "435134"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(kunlun.Connection{})))
	require.Equal(t, "StartGetTaskStatus", dex.GetFinalStepType[kunlun.GetTaskStatusInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := kunlunmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
