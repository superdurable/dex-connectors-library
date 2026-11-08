// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package boardinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/miro/go"
	"github.com/superdurable/dex-connectors-library/connectors/miro/go/miromock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestBoardInspectionFlowIdentityAndMapping(t *testing.T) {
	input := miro.GetBoardInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(miro.Connection{})))
	require.Equal(t, "StartGetBoard", dex.GetFinalStepType[miro.GetBoardInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := miromock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
