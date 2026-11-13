// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package uservideos

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/kuaishou/go"
	"github.com/superdurable/dex-connectors-library/connectors/kuaishou/go/kuaishoumock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestUserVideosPageFlowIdentityAndMapping(t *testing.T) {
	input := kuaishou.ListUserVideosInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(kuaishou.Connection{})))
	require.Equal(t, "StartListUserVideos", dex.GetFinalStepType[kuaishou.ListUserVideosInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := kuaishoumock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
