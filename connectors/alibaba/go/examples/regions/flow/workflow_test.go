// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package regionsflow

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/alibaba/go"
	"github.com/superdurable/dex-connectors-library/connectors/alibaba/go/alibabamock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestRegionsFlowIdentityAndMapping(t *testing.T) {
	input := alibaba.DescribeRegionsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(alibaba.Connection{})))
	require.Equal(t, "StartDescribeRegions", dex.GetFinalStepType[alibaba.DescribeRegionsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := alibabamock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
