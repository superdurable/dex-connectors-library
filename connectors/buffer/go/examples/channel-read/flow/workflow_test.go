// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package channelread

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/buffer/go"
	"github.com/superdurable/dex-connectors-library/connectors/buffer/go/buffermock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestChannelReadFlowIdentityAndMapping(t *testing.T) {
	input := buffer.GetChannelInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(buffer.Connection{})))
	require.Equal(t, "StartGetChannel", dex.GetFinalStepType[buffer.GetChannelInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := buffermock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
