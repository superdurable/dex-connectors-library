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

func TestSubscriberPageFlowIdentityAndMapping(t *testing.T) {
	input := drip.ListSubscribersInput{AccountID: "9999999", Page: 1, PerPage: 100}
	require.Equal(t, input, mapSubscriberPageInput(input))
	require.Equal(t, SubscriberPageFlowType, dex.GetFinalFlowType(NewSubscriberPageFlow(drip.Connection{})))
	require.Equal(t, "StartSubscriberPage", dex.GetFinalStepType[drip.ListSubscribersInput](startSubscriberPage{}))
	wait, err := startSubscriberPage{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := dripmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewSubscriberPageFlow(mock.Connection())})
	require.NoError(t, err)
}
