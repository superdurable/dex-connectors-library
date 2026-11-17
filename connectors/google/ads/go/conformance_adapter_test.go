// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ads

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "google-ads", Name: "google-ads"}
	credentials := Credentials{
		OAuthClientID: "synthetic-client-id", OAuthClientSecret: sdkgo.NewSecretString("synthetic-client-secret"),
		AccessToken: sdkgo.NewSecretString("test-google-ads-token"), RefreshToken: sdkgo.NewSecretString("synthetic-refresh-token"),
	}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	mutationContext := singledispatchtest.NewContext("google-ads-provider-test", "mutation")
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			mutationContext.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"listAccessibleCustomers": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListAccessibleCustomers(), reference),
		"listCampaigns":           newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListCampaigns(), reference),
		"getCampaign":             newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCampaign(), reference),
		"createCampaign":          newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.CreateCampaign(), reference, scenario.CallID),
		"updateCampaignStatus":    newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.UpdateCampaignStatus(), reference, scenario.CallID),
		"removeCampaign":          newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.RemoveCampaign(), reference, scenario.CallID),
	}}
}
