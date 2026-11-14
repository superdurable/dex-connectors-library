// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package productlist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zentao/go"
	"github.com/superdurable/dex-connectors-library/connectors/zentao/go/zentaomock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestProductsFlowIdentityAndMapping(t *testing.T) {
	input := zentao.ListProductsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(zentao.Connection{})))
	require.Equal(t, "StartListProducts", dex.GetFinalStepType[zentao.ListProductsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := zentaomock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
