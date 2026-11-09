// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workerinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/workday/go"
	"github.com/superdurable/dex-connectors-library/connectors/workday/go/workdaymock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestWorkerInspectionFlowIdentityAndMapping(t *testing.T) {
	input := workday.GetWorkerInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(workday.Connection{})))
	require.Equal(t, "StartGetWorker", dex.GetFinalStepType[workday.GetWorkerInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := workdaymock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
