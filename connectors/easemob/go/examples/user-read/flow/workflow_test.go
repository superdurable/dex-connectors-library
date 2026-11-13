// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package userread

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/easemob/go"
	"github.com/superdurable/dex-connectors-library/connectors/easemob/go/easemobmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestUserReadFlowIdentityAndMapping(t *testing.T) {
	input := easemob.GetUserDetailInput{UserID: "user1"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(easemob.Connection{})))
	require.Equal(t, "StartGetUserDetail", dex.GetFinalStepType[easemob.GetUserDetailInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := easemobmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
