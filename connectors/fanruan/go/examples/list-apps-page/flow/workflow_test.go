// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package appspage

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/fanruan/go"
	"github.com/superdurable/dex-connectors-library/connectors/fanruan/go/fanruanmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListAppsPageFlowIdentityAndMapping(t *testing.T) {
	input := fanruan.ListAppsPageInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(fanruan.Connection{})))
	require.Equal(t, "StartListAppsPage", dex.GetFinalStepType[fanruan.ListAppsPageInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := fanruanmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
