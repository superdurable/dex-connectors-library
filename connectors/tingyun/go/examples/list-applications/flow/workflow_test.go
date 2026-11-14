// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package applications

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/tingyun/go"
	"github.com/superdurable/dex-connectors-library/connectors/tingyun/go/tingyunmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListApplicationsFlowIdentityAndMapping(t *testing.T) {
	input := tingyun.ListApplicationsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(tingyun.Connection{})))
	require.Equal(t, "StartListApplications", dex.GetFinalStepType[tingyun.ListApplicationsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := tingyunmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
