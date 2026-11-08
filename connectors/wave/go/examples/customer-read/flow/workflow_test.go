// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerread

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/wave/go"
	"github.com/superdurable/dex-connectors-library/connectors/wave/go/wavemock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestCustomerReadFlowIdentityAndMapping(t *testing.T) {
	input := wave.GetCustomerInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(wave.Connection{})))
	require.Equal(t, "StartGetCustomer", dex.GetFinalStepType[wave.GetCustomerInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := wavemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
