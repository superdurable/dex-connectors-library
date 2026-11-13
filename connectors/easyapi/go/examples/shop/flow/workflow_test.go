// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shopflow

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/easyapi/go"
	"github.com/superdurable/dex-connectors-library/connectors/easyapi/go/easyapimock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestShopFlowIdentityAndMapping(t *testing.T) {
	input := easyapi.QueryShopInput{TaxNumber: "91320211MA1WML8X6T"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(easyapi.Connection{})))
	require.Equal(t, "StartQueryShop", dex.GetFinalStepType[easyapi.QueryShopInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := easyapimock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
