// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package regionlist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/volcengine/go"
	"github.com/superdurable/dex-connectors-library/connectors/volcengine/go/volcenginemock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestRegionListFlowIdentityAndMapping(t *testing.T) {
	input := volcengine.DescribeRegionsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(volcengine.Connection{})))
	require.Equal(t, "StartDescribeRegions", dex.GetFinalStepType[volcengine.DescribeRegionsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := volcenginemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
