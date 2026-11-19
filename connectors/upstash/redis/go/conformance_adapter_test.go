// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package redis

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
	reference := sdkgo.ConnectionRef{Provider: "upstash-redis", Name: "upstash-redis"}
	credentials := sdkgo.StaticCredentialProvider[Credentials]{
		reference: {RestToken: sdkgo.NewSecretString("sample-redis-token")},
	}
	if scenario.Name == "getInfo-defect" {
		credentials[reference] = Credentials{}
	}
	client, err := New(Config{BaseURL: endpoint}, credentials, WithHTTPClient(transport))
	require.NoError(t, err)
	context := &redisTestContext{}
	if len(scenario.PriorHeartbeat) > 0 {
		context.heartbeat = append(json.RawMessage(nil), scenario.PriorHeartbeat...)
		var marker struct {
			Attempt int32 `json:"attempt"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &marker))
		context.attempt = marker.Attempt + 1
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.attempt = int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"getKey":         newConformanceQueryInvocation(t, scenario.Input, context, client.GetKey(), reference),
		"setKey":         newConformanceMutationInvocation(t, scenario.Input, context, client.SetKey(), reference, scenario.CallID),
		"deleteKey":      newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteKey(), reference, scenario.CallID),
		"listKeys":       newConformanceQueryInvocation(t, scenario.Input, context, client.ListKeys(), reference),
		"getListLength":  newConformanceQueryInvocation(t, scenario.Input, context, client.GetListLength(), reference),
		"getInfo":        newConformanceQueryInvocation(t, scenario.Input, context, client.GetInfo(), reference),
		"pushListItem":   newConformanceMutationInvocation(t, scenario.Input, context, client.PushListItem(), reference, scenario.CallID),
		"publishMessage": newConformanceMutationInvocation(t, scenario.Input, context, client.PublishMessage(), reference, scenario.CallID),
	}}
}
