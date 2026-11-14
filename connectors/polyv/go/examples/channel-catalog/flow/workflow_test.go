// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package channelcatalog

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/polyv/go"
	"github.com/superdurable/dex-connectors-library/connectors/polyv/go/polyvmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestChannelCatalogFlowIdentityAndMapping(t *testing.T) {
	input := polyv.ListChannelsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(polyv.Connection{})))
	require.Equal(t, "StartListChannels", dex.GetFinalStepType[polyv.ListChannelsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := polyvmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
