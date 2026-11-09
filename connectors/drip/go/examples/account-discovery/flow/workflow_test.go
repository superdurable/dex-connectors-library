// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountdiscovery

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/drip/go"
	"github.com/superdurable/dex-connectors-library/connectors/drip/go/dripmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestAccountDiscoveryFlowIdentityAndMapping(t *testing.T) {
	input := drip.ListAccountsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(drip.Connection{})))
	require.Equal(t, "StartListAccounts", dex.GetFinalStepType[drip.ListAccountsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := dripmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
