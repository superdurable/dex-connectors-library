// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drip

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = "/v2"
const conformanceFixturePathPrefix = "/v2"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "drip", Name: "drip"}
	credentials := sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString("test-token")}}
	if scenario.Name == "listAccounts-defect" {
		credentials = sdkgo.StaticCredentialProvider[Credentials]{}
	}
	client, err := New(Config{BaseURL: endpoint}, credentials, WithHTTPClient(transport))
	require.NoError(t, err)
	context := singledispatchtest.NewContext("drip-provider-test", "subscription")
	priorAttempt := 0
	if len(scenario.PriorHeartbeat) > 0 {
		var marker struct {
			Attempt int             `json:"attempt"`
			Value   json.RawMessage `json:"value"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &marker))
		checkpoint := scenario.PriorHeartbeat
		if len(marker.Value) > 0 {
			checkpoint = marker.Value
		}
		require.NoError(t, context.SetPersistedCheckpoint(checkpoint))
		priorAttempt = marker.Attempt
		for range priorAttempt {
			context.AdvanceAttempt()
		}
	}
	currentAttempt := priorAttempt + 1
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		for currentAttempt < priorAttempt+attempt {
			context.AdvanceAttempt()
			currentAttempt++
		}
	}, actions: map[string]conformanceOperationInvocation{
		"listCampaignSubscribers":             newConformanceQueryInvocation(t, scenario.Input, context, client.ListCampaignSubscribers(), reference),
		"listSubscriberCampaignSubscriptions": newConformanceQueryInvocation(t, scenario.Input, context, client.ListSubscriberCampaignSubscriptions(), reference),
		"subscribeToCampaign":                 newConformanceMutationInvocation(t, scenario.Input, context, client.SubscribeToCampaign(), reference, scenario.CallID),
		"removeSubscriberFromCampaign":        newConformanceMutationInvocation(t, scenario.Input, context, client.RemoveSubscriberFromCampaign(), reference, scenario.CallID),
		"unsubscribeSubscriberFromAll":        newConformanceMutationInvocation(t, scenario.Input, context, client.UnsubscribeSubscriberFromAll(), reference, scenario.CallID),
		"listCampaigns":                       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListCampaigns(), reference),
		"getCampaign":                         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCampaign(), reference),
		"activateCampaign":                    newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.ActivateCampaign(), reference, scenario.CallID),
		"pauseCampaign":                       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.PauseCampaign(), reference, scenario.CallID),
		"listAccounts":                        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListAccounts(), reference),
		"getSubscriber":                       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetSubscriber(), reference),
		"listSubscribers":                     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListSubscribers(), reference),
		"upsertSubscriber":                    newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpsertSubscriber(), reference, scenario.CallID),
		"deleteSubscriber":                    newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteSubscriber(), reference, scenario.CallID),
	}}
}
