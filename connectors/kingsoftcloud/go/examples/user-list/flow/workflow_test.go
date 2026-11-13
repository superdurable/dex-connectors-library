// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package userlist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/kingsoftcloud/go"
	"github.com/superdurable/dex-connectors-library/connectors/kingsoftcloud/go/kingsoftcloudmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestUserListFlowIdentityAndMapping(t *testing.T) {
	input := kingsoftcloud.ListUsersInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(kingsoftcloud.Connection{})))
	require.Equal(t, "StartListUsers", dex.GetFinalStepType[kingsoftcloud.ListUsersInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := kingsoftcloudmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
