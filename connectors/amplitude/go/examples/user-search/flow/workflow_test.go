// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package usersearch

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amplitude/go"
	"github.com/superdurable/dex-connectors-library/connectors/amplitude/go/amplitudemock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestUserSearchFlowIdentityAndMapping(t *testing.T) {
	input := amplitude.SearchUsersInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(amplitude.Connection{})))
	require.Equal(t, "StartSearchUsers", dex.GetFinalStepType[amplitude.SearchUsersInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := amplitudemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
