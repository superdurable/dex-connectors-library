// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectlist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/pingcap/go"
	"github.com/superdurable/dex-connectors-library/connectors/pingcap/go/pingcapmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestProjectListFlowIdentityAndMapping(t *testing.T) {
	input := pingcap.ListProjectsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(pingcap.Connection{})))
	require.Equal(t, "StartListProjects", dex.GetFinalStepType[pingcap.ListProjectsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := pingcapmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
