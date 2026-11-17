// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerio

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "customerio", Name: "customerio"}
	privateKey := "sample-customerio-private-key"
	if scenario.Name == "listCampaigns-defect" {
		privateKey = ""
	}
	client, err := New(Config{BaseURL: endpoint, TrackingBaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {PrivateKey: sdkgo.NewSecretString(privateKey), TrackingSiteID: "sample-tracking-site-id", TrackingAPIKey: sdkgo.NewSecretString("sample-tracking-api-key")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := &eventConformanceContext{attempt: 1}
	if len(scenario.PriorHeartbeat) > 0 {
		context.heartbeat = append([]byte(nil), scenario.PriorHeartbeat...)
		var prior struct {
			Attempt int32 `json:"attempt"`
		}
		require.NoError(t, json.Unmarshal(context.heartbeat, &prior))
		context.attempt = prior.Attempt + 1
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.attempt = int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"getCustomersByEmail": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCustomersByEmail(), reference),
		"getCampaign":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCampaign(), reference),
		"listCampaigns":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListCampaigns(), reference),
		"getCampaignMetrics":  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCampaignMetrics(), reference),
		"upsertCustomer":      newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpsertCustomer(), reference, scenario.CallID),
		"deleteCustomer":      newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteCustomer(), reference, scenario.CallID),
		"trackCustomerEvent":  newConformanceMutationInvocation(t, scenario.Input, context, client.TrackCustomerEvent(), reference, scenario.CallID),
		"trackAnonymousEvent": newConformanceMutationInvocation(t, scenario.Input, context, client.TrackAnonymousEvent(), reference, scenario.CallID),
	}}
}

type eventConformanceContext struct {
	queryContext
	attempt   int32
	heartbeat []byte
}

func (context *eventConformanceContext) Attempt() int32 { return context.attempt }
func (context *eventConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err == nil {
		context.heartbeat = encoded
	}
	return err
}
func (context *eventConformanceContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, target)
}
