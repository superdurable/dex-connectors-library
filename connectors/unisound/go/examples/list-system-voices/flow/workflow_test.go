// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package systemvoices

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/unisound/go"
	"github.com/superdurable/dex-connectors-library/connectors/unisound/go/unisoundmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListSystemVoicesFlowIdentityAndMapping(t *testing.T) {
	input := unisound.ListSystemVoicesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(unisound.Connection{})))
	require.Equal(t, "StartListSystemVoices", dex.GetFinalStepType[unisound.ListSystemVoicesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := unisoundmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
