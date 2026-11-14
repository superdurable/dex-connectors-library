// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package useronlinestatus

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/rongcloud/go"
	"github.com/superdurable/dex-connectors-library/connectors/rongcloud/go/rongcloudmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestUserOnlineStatusFlowIdentityAndMapping(t *testing.T) {
	input := rongcloud.CheckUserOnlineInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(rongcloud.Connection{})))
	require.Equal(t, "StartCheckUserOnline", dex.GetFinalStepType[rongcloud.CheckUserOnlineInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := rongcloudmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
