// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package modelinspection

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/anthropic/go"
	"github.com/superdurable/dex-connectors-library/connectors/anthropic/go/anthropicmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestModelInspectionFlowIdentityAndMapping(t *testing.T) {
	input := anthropic.GetModelInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(anthropic.Connection{})))
	require.Equal(t, "StartGetModel", dex.GetFinalStepType[anthropic.GetModelInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := anthropicmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
