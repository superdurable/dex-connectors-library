// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package campaigndiscovery

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/advertising/go"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/advertising/go/microsoftadvertisingmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestCampaignDiscoveryFlowIdentityAndMapping(t *testing.T) {
	input := microsoftadvertising.ListCampaignsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(microsoftadvertising.Connection{})))
	require.Equal(t, "StartListCampaigns", dex.GetFinalStepType[microsoftadvertising.ListCampaignsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := microsoftadvertisingmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}

func TestAdGroupDiscoveryFlowRegisters(t *testing.T) {
	mock := microsoftadvertisingmock.New(t, ConnectionName)
	_, err := dex.NewRegistry([]dex.Flow{NewAdGroupDiscoveryFlow(mock.Connection())})
	require.NoError(t, err)
	wait, err := startAdGroupDiscovery{}.WaitFor(nil, microsoftadvertising.ListAdGroupsInput{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestKeywordDiscoveryFlowRegisters(t *testing.T) {
	mock := microsoftadvertisingmock.New(t, ConnectionName)
	_, err := dex.NewRegistry([]dex.Flow{NewKeywordDiscoveryFlow(mock.Connection())})
	require.NoError(t, err)
	wait, err := startKeywordDiscovery{}.WaitFor(nil, microsoftadvertising.ListKeywordsInput{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestAdDiscoveryFlowRegisters(t *testing.T) {
	mock := microsoftadvertisingmock.New(t, ConnectionName)
	_, err := dex.NewRegistry([]dex.Flow{NewAdDiscoveryFlow(mock.Connection())})
	require.NoError(t, err)
	wait, err := startAdDiscovery{}.WaitFor(nil, microsoftadvertising.ListAdsInput{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}
