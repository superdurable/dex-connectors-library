// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tiktok

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = "/open_api/v1.3"
const conformanceFixturePathPrefix = "/open_api/v1.3"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "tiktok-business", Name: "tiktok-business"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {AccessToken: sdkgo.NewSecretString("sample-tiktok-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	createContext := singledispatchtest.NewContext("tiktok-conformance", "create-campaign")
	createAdgroupContext := singledispatchtest.NewContext("tiktok-conformance", "create-adgroup")
	createAdContext := singledispatchtest.NewContext("tiktok-conformance", "create-ad")
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			createContext.AdvanceAttempt()
			createAdgroupContext.AdvanceAttempt()
			createAdContext.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"createAd":             newConformanceMutationInvocation(t, scenario.Input, createAdContext, client.CreateAd(), reference, scenario.CallID),
		"updateAd":             newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateAd(), reference, scenario.CallID),
		"createAdgroup":        newConformanceMutationInvocation(t, scenario.Input, createAdgroupContext, client.CreateAdgroup(), reference, scenario.CallID),
		"updateAdgroup":        newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateAdgroup(), reference, scenario.CallID),
		"createCampaign":       newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateCampaign(), reference, scenario.CallID),
		"updateCampaign":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateCampaign(), reference, scenario.CallID),
		"updateCampaignStatus": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateCampaignStatus(), reference, scenario.CallID),
		"updateAdgroupStatus":  newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateAdgroupStatus(), reference, scenario.CallID),
		"updateAdStatus":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateAdStatus(), reference, scenario.CallID),
		"listAdgroups":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListAdgroups(), reference),
		"listAds":              newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListAds(), reference),
		"listCampaigns":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListCampaigns(), reference),
	}}
}
