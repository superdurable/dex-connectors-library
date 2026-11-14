// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package modelcatalog

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/tokengp/go"
	"github.com/superdurable/dex-connectors-library/connectors/tokengp/go/tokengpmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestModelCatalogFlowIdentityAndMapping(t *testing.T) {
	input := tokengp.ListModelsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(tokengp.Connection{})))
	require.Equal(t, "StartListModels", dex.GetFinalStepType[tokengp.ListModelsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := tokengpmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
