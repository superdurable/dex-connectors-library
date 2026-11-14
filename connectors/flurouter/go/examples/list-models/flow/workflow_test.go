// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package models

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/flurouter/go"
	"github.com/superdurable/dex-connectors-library/connectors/flurouter/go/fluroutermock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListModelsFlowIdentityAndMapping(t *testing.T) {
	input := flurouter.ListModelsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(flurouter.Connection{})))
	require.Equal(t, "StartListModels", dex.GetFinalStepType[flurouter.ListModelsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := fluroutermock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
