// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contractinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/deel/go"
	"github.com/superdurable/dex-connectors-library/connectors/deel/go/deelmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestContractInspectionFlowIdentityAndMapping(t *testing.T) {
	input := deel.GetContractInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(deel.Connection{})))
	require.Equal(t, "StartGetContract", dex.GetFinalStepType[deel.GetContractInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := deelmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
