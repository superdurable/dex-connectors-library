// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package usageread

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/upyun/go"
	"github.com/superdurable/dex-connectors-library/connectors/upyun/go/upyunmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestUsageReadFlowIdentityAndMapping(t *testing.T) {
	input := upyun.GetServiceUsageInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(upyun.Connection{})))
	require.Equal(t, "StartGetServiceUsage", dex.GetFinalStepType[upyun.GetServiceUsageInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := upyunmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
