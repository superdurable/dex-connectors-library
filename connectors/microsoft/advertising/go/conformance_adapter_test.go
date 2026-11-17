// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package microsoftadvertising

import (
	"encoding/json"
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
	reference := sdkgo.ConnectionRef{Provider: "microsoft-advertising", Name: "microsoft-advertising"}
	client, err := New(Config{BaseURL: endpoint, CustomerID: "73456"}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {
			OAuthClientID: "synthetic-client-id", OAuthClientSecret: sdkgo.NewSecretString("synthetic-client-secret"),
			AccessToken: sdkgo.NewSecretString("test-microsoft-advertising-token"), RefreshToken: sdkgo.NewSecretString("synthetic-refresh-token"), DeveloperToken: sdkgo.NewSecretString("test-developer-token"),
		},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := singledispatchtest.NewContext("advertising-conformance", "campaign-operation")
	prior := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(raw []byte) {
		var checkpoint any
		require.NoError(t, json.Unmarshal(raw, &checkpoint))
		require.NoError(t, context.SetPersistedCheckpoint(checkpoint))
	})
	currentAttempt := int32(1)
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		for currentAttempt < prior+int32(attempt) {
			context.AdvanceAttempt()
			currentAttempt++
		}
	}, actions: map[string]conformanceOperationInvocation{
		"listAdGroups":   newConformanceQueryInvocation(t, scenario.Input, context, client.ListAdGroups(), reference),
		"getAdGroup":     newConformanceQueryInvocation(t, scenario.Input, context, client.GetAdGroup(), reference),
		"createAdGroup":  newConformanceMutationInvocation(t, scenario.Input, context, client.CreateAdGroup(), reference, scenario.CallID),
		"updateAdGroup":  newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateAdGroup(), reference, scenario.CallID),
		"deleteAdGroup":  newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteAdGroup(), reference, scenario.CallID),
		"listCampaigns":  newConformanceQueryInvocation(t, scenario.Input, context, client.ListCampaigns(), reference),
		"getCampaign":    newConformanceQueryInvocation(t, scenario.Input, context, client.GetCampaign(), reference),
		"createCampaign": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateCampaign(), reference, scenario.CallID),
		"updateCampaign": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateCampaign(), reference, scenario.CallID),
		"deleteCampaign": newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteCampaign(), reference, scenario.CallID),
	}}
}
