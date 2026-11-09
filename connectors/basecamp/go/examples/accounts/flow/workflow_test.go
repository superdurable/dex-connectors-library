// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accounts

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/basecamp/go"
	"github.com/superdurable/dex-connectors-library/connectors/basecamp/go/basecampmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestAccountsFlowIdentityAndMapping(t *testing.T) {
	input := basecamp.ListAccountsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(basecamp.Connection{})))
	require.Equal(t, "StartListAccounts", dex.GetFinalStepType[basecamp.ListAccountsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := basecampmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
