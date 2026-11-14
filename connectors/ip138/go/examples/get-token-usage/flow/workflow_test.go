// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tokenusage

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/ip138/go"
	"github.com/superdurable/dex-connectors-library/connectors/ip138/go/ip138mock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetTokenUsageFlowIdentityAndMapping(t *testing.T) {
	input := ip138.GetTokenUsageInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(ip138.Connection{})))
	require.Equal(t, "StartGetTokenUsage", dex.GetFinalStepType[ip138.GetTokenUsageInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := ip138mock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
