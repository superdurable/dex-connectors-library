// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shopify

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = "/admin/api/2026-10"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	_ = endpoint // Shopify's API host is fixed; the recorder intercepts its transport.
	client, err := New(Config{ShopSubdomain: "test-shop"}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {AccessToken: sdkgo.NewSecretString("test-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	mutationContext := &mutationConformanceContext{attempt: 1}
	priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { mutationContext.heartbeat = value })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { mutationContext.attempt = priorAttempt + int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"getOrder":      newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetOrder(), testConnection),
		"listOrders":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListOrders(), testConnection),
		"countOrders":   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.CountOrders(), testConnection),
		"createOrder":   newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.CreateOrder(), testConnection, scenario.CallID),
		"updateOrder":   newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.UpdateOrder(), testConnection, scenario.CallID),
		"deleteOrder":   newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.DeleteOrder(), testConnection, scenario.CallID),
		"getProduct":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetProduct(), testConnection),
		"listProducts":  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListProducts(), testConnection),
		"createProduct": newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.CreateProduct(), testConnection, scenario.CallID),
		"updateProduct": newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.UpdateProduct(), testConnection, scenario.CallID),
		"deleteProduct": newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.DeleteProduct(), testConnection, scenario.CallID),
	}}
}

type mutationConformanceContext struct {
	queryContext
	attempt   int32
	heartbeat json.RawMessage
}

func (context *mutationConformanceContext) Attempt() int32 { return context.attempt }

func (context *mutationConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.heartbeat = encoded
	return nil
}

func (context *mutationConformanceContext) GetLastHeartbeatValue(destination any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, destination)
}
