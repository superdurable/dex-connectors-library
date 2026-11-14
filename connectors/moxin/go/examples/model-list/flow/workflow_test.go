// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package modellist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/moxin/go"
	"github.com/superdurable/dex-connectors-library/connectors/moxin/go/moxinmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestModelsFlowIdentityAndMapping(t *testing.T) {
	input := moxin.ListModelsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(moxin.Connection{})))
	require.Equal(t, "StartListModels", dex.GetFinalStepType[moxin.ListModelsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := moxinmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
