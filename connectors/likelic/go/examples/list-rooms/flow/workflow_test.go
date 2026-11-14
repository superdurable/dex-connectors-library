// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package roomcatalog

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/likelic/go"
	"github.com/superdurable/dex-connectors-library/connectors/likelic/go/likelicmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListRoomsFlowIdentityAndMapping(t *testing.T) {
	input := likelic.ListRoomsInput{Page: 1}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(likelic.Connection{})))
	require.Equal(t, "StartListRooms", dex.GetFinalStepType[likelic.ListRoomsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := likelicmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
