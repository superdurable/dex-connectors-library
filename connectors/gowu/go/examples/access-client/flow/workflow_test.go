// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accessclientflow

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/gowu/go"
	"github.com/superdurable/dex-connectors-library/connectors/gowu/go/gowumock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestAccessClientFlowIdentityAndMapping(t *testing.T) {
	input := gowu.GetAccessClientInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(gowu.Connection{})))
	require.Equal(t, "StartGetAccessClient", dex.GetFinalStepType[gowu.GetAccessClientInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := gowumock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
