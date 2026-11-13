// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package listmodels

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/infiniai/go"
	"github.com/superdurable/dex-connectors-library/connectors/infiniai/go/infiniaimock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListModelsFlowIdentityAndMapping(t *testing.T) {
	input := infiniai.ListModelsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(infiniai.Connection{})))
	require.Equal(t, "StartListModels", dex.GetFinalStepType[infiniai.ListModelsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := infiniaimock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
