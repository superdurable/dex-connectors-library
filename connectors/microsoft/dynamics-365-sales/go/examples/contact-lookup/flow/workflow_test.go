// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactlookup

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/dynamics-365-sales/go"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/dynamics-365-sales/go/dynamics365salesmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestContactFlowInputAndMapping(t *testing.T) {
	input := dynamics365sales.FindContactInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(dynamics365sales.Connection{})))
	require.Equal(t, "StartFindContact", dex.GetFinalStepType[dynamics365sales.FindContactInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := dynamics365salesmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
