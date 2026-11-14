// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package applicationslist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/qingflow/go"
	"github.com/superdurable/dex-connectors-library/connectors/qingflow/go/qingflowmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListApplicationsFlowIdentityAndMapping(t *testing.T) {
	input := qingflow.ListApplicationsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(qingflow.Connection{})))
	require.Equal(t, "StartListApplications", dex.GetFinalStepType[qingflow.ListApplicationsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := qingflowmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
