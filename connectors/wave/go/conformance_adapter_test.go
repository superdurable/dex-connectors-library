// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wave

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
	reference := sdkgo.ConnectionRef{Provider: "wave", Name: "wave"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {AccessToken: sdkgo.NewSecretString("sample-wave-access-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	ctx := &customerTestContext{}
	if len(scenario.PriorHeartbeat) > 0 {
		ctx.heartbeat = append(json.RawMessage(nil), scenario.PriorHeartbeat...)
		var marker struct {
			Attempt int32 `json:"attempt"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &marker))
		ctx.attempt = marker.Attempt + 1
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			ctx.attempt = int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"getCustomer":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCustomer(), reference),
		"listCustomers":  newConformanceQueryInvocation(t, scenario.Input, ctx, client.ListCustomers(), reference),
		"createCustomer": newConformanceMutationInvocation(t, scenario.Input, ctx, client.CreateCustomer(), reference, scenario.CallID),
		"updateCustomer": newConformanceMutationInvocation(t, scenario.Input, ctx, client.UpdateCustomer(), reference, scenario.CallID),
		"deleteCustomer": newConformanceMutationInvocation(t, scenario.Input, ctx, client.DeleteCustomer(), reference, scenario.CallID),
		"listProducts":   newConformanceQueryInvocation(t, scenario.Input, ctx, client.ListProducts(), reference),
		"getProduct":     newConformanceQueryInvocation(t, scenario.Input, ctx, client.GetProduct(), reference),
		"createProduct":  newConformanceMutationInvocation(t, scenario.Input, ctx, client.CreateProduct(), reference, scenario.CallID),
		"updateProduct":  newConformanceMutationInvocation(t, scenario.Input, ctx, client.UpdateProduct(), reference, scenario.CallID),
		"archiveProduct": newConformanceMutationInvocation(t, scenario.Input, ctx, client.ArchiveProduct(), reference, scenario.CallID),
		"listInvoices":   newConformanceQueryInvocation(t, scenario.Input, ctx, client.ListInvoices(), reference),
		"getInvoice":     newConformanceQueryInvocation(t, scenario.Input, ctx, client.GetInvoice(), reference),
		"createInvoice":  newConformanceMutationInvocation(t, scenario.Input, ctx, client.CreateInvoice(), reference, scenario.CallID),
		"updateInvoice":  newConformanceMutationInvocation(t, scenario.Input, ctx, client.UpdateInvoice(), reference, scenario.CallID),
		"approveInvoice": newConformanceMutationInvocation(t, scenario.Input, ctx, client.ApproveInvoice(), reference, scenario.CallID),
		"deleteInvoice":  newConformanceMutationInvocation(t, scenario.Input, ctx, client.DeleteInvoice(), reference, scenario.CallID),
		"sendInvoice":    newConformanceMutationInvocation(t, scenario.Input, ctx, client.SendInvoice(), reference, scenario.CallID),
	}}
}
