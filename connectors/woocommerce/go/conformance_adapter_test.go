// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package woocommerce

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
	connection := sdkgo.ConnectionRef{Provider: "woocommerce", Name: "woocommerce"}
	client, err := New(Config{StoreURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{connection: {ConsumerKey: sdkgo.NewSecretString("ck_test"), ConsumerSecret: sdkgo.NewSecretString("cs_test")}}, WithHTTPClient(transport))
	require.NoError(t, err)
	mutationContext := &orderMutationContext{queryContext: queryContext{}}
	if len(scenario.PriorHeartbeat) != 0 {
		var marker struct {
			Attempt int32           `json:"attempt"`
			Value   json.RawMessage `json:"value"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &marker))
		mutationContext.heartbeat = append([]byte(nil), marker.Value...)
		mutationContext.attempt = marker.Attempt + 1
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			mutationContext.attempt = int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"listOrders":  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListOrders(), connection),
		"getOrder":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetOrder(), connection),
		"createOrder": newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.CreateOrder(), connection, scenario.CallID),
		"updateOrder": newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.UpdateOrder(), connection, scenario.CallID),
		"deleteOrder": newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.DeleteOrder(), connection, scenario.CallID),
	}}
}

type orderMutationContext struct {
	queryContext
	attempt   int32
	heartbeat []byte
}

func (context *orderMutationContext) Attempt() int32 {
	if context.attempt == 0 {
		return 1
	}
	return context.attempt
}

func (context *orderMutationContext) RecordHeartbeat(value any) error {
	if value == nil {
		context.heartbeat = nil
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.heartbeat = encoded
	return nil
}

func (context *orderMutationContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, target)
}
