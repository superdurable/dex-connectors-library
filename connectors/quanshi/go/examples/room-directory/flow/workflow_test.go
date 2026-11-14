// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package roomcatalog

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/quanshi/go"
	"github.com/superdurable/dex-connectors-library/connectors/quanshi/go/quanshimock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestRoomCatalogFlowIdentityAndMapping(t *testing.T) {
	input := quanshi.ListRoomsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(quanshi.Connection{})))
	require.Equal(t, "StartListRooms", dex.GetFinalStepType[quanshi.ListRoomsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := quanshimock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
