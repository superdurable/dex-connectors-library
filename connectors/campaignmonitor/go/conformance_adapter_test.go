// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package campaignmonitor

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/api/v3.3"
const conformanceFixturePathPrefix = "/api/v3.3"

type campaignMonitorConformanceContext struct {
	queryContext
	attempt  int32
	prior    json.RawMessage
	recorded json.RawMessage
}

func (context *campaignMonitorConformanceContext) Attempt() int32 { return context.attempt }

func (context *campaignMonitorConformanceContext) GetLastHeartbeatValue(value any) (bool, error) {
	if len(context.prior) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.prior, value)
}

func (context *campaignMonitorConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.recorded = encoded
	return nil
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "campaignmonitor", Name: "campaignmonitor"}
	credentials := sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString("test-token")}}
	if scenario.Name == "listClients-defect" {
		credentials = sdkgo.StaticCredentialProvider[Credentials]{}
	}
	client, err := New(Config{BaseURL: endpoint}, credentials, WithHTTPClient(transport))
	require.NoError(t, err)
	context := &campaignMonitorConformanceContext{attempt: 1}
	if len(scenario.PriorHeartbeat) > 0 {
		context.attempt = conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { context.prior = value }) + 1
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.prior = append(json.RawMessage(nil), context.recorded...)
			context.recorded = nil
			context.attempt = int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"getList":             newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetList(), reference),
		"createList":          newConformanceMutationInvocation(t, scenario.Input, context, client.CreateList(), reference, scenario.CallID),
		"updateList":          newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateList(), reference, scenario.CallID),
		"deleteList":          newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteList(), reference, scenario.CallID),
		"listSubscribers":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListSubscribers(), reference),
		"listClients":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListClients(), reference),
		"listSubscriberLists": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListSubscriberLists(), reference),
		"getSubscriber":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetSubscriber(), reference),
		"upsertSubscriber":    newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpsertSubscriber(), reference, scenario.CallID),
		"updateSubscriber":    newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateSubscriber(), reference, scenario.CallID),
		"deleteSubscriber":    newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteSubscriber(), reference, scenario.CallID),
	}}
}
