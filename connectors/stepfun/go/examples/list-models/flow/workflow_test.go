// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package listmodels

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/stepfun/go"
	"github.com/superdurable/dex-connectors-library/connectors/stepfun/go/stepfunmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListModelsFlowIdentityAndMapping(t *testing.T) {
	input := stepfun.ListModelsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(stepfun.Connection{})))
	require.Equal(t, "StartListModels", dex.GetFinalStepType[stepfun.ListModelsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := stepfunmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
