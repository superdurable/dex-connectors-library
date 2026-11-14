// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spacelist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/vika/go"
	"github.com/superdurable/dex-connectors-library/connectors/vika/go/vikamock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestSpacesFlowIdentityAndMapping(t *testing.T) {
	input := vika.ListSpacesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(vika.Connection{})))
	require.Equal(t, "StartListSpaces", dex.GetFinalStepType[vika.ListSpacesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := vikamock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
