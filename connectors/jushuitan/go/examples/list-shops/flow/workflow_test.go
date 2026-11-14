// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package listshops

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/jushuitan/go"
	"github.com/superdurable/dex-connectors-library/connectors/jushuitan/go/jushuitanmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListShopsFlowIdentityAndMapping(t *testing.T) {
	input := jushuitan.ListShopsInput{PageIndex: 1, PageSize: 10}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(jushuitan.Connection{})))
	require.Equal(t, "StartListShops", dex.GetFinalStepType[jushuitan.ListShopsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := jushuitanmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
