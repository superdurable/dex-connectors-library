// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package listjobfunctions

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/moka/go"
	"github.com/superdurable/dex-connectors-library/connectors/moka/go/mokamock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListJobFunctionsFlowIdentityAndMapping(t *testing.T) {
	input := moka.ListJobFunctionsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(moka.Connection{})))
	require.Equal(t, "StartListJobFunctions", dex.GetFinalStepType[moka.ListJobFunctionsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := mokamock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
