// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package industries

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/shixiseng/go"
	"github.com/superdurable/dex-connectors-library/connectors/shixiseng/go/shixisengmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListIndustriesFlowIdentityAndMapping(t *testing.T) {
	input := shixiseng.ListIndustriesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(shixiseng.Connection{})))
	require.Equal(t, "StartListIndustries", dex.GetFinalStepType[shixiseng.ListIndustriesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := shixisengmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
