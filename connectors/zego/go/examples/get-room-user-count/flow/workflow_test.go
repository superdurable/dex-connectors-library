// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package roomcount

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zego/go"
	"github.com/superdurable/dex-connectors-library/connectors/zego/go/zegomock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetRoomUserCountFlowIdentityAndMapping(t *testing.T) {
	input := zego.GetRoomUserCountInput{RoomID: "room-1"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(zego.Connection{})))
	require.Equal(t, "StartGetRoomUserCount", dex.GetFinalStepType[zego.GetRoomUserCountInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := zegomock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
