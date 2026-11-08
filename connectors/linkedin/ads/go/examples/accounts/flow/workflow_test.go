// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accounts

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linkedin/ads/go"
	"github.com/superdurable/dex-connectors-library/connectors/linkedin/ads/go/adsmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestAccountsFlowIdentityAndMapping(t *testing.T) {
	input := ads.ListAdAccountsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(ads.Connection{})))
	require.Equal(t, "StartListAdAccounts", dex.GetFinalStepType[ads.ListAdAccountsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := adsmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
