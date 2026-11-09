// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package job_runinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/databricks/go"
	"github.com/superdurable/dex-connectors-library/connectors/databricks/go/databricksmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestJobRunInspectionFlowIdentityAndMapping(t *testing.T) {
	input := databricks.GetJobRunInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(databricks.Connection{})))
	require.Equal(t, "StartGetJobRun", dex.GetFinalStepType[databricks.GetJobRunInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := databricksmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
