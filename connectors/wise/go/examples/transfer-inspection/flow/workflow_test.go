// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package transferinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/wise/go"
	"github.com/superdurable/dex-connectors-library/connectors/wise/go/wisemock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTransferInspectionFlowIdentityAndMapping(t *testing.T) {
	input := wise.GetTransferInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(wise.Connection{})))
	require.Equal(t, "StartGetTransfer", dex.GetFinalStepType[wise.GetTransferInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := wisemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
