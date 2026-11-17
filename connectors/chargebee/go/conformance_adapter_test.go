// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package chargebee

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/api"
const conformanceFixturePathPrefix = "/api"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "chargebee", Name: "chargebee"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("sample-chargebee-api-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getSubscription":   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetSubscription(), reference),
		"listSubscriptions": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListSubscriptions(), reference),
		"getCustomer":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCustomer(), reference),
		"listCustomers":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListCustomers(), reference),
		"getInvoice":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetInvoice(), reference),
		"listInvoices":      newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListInvoices(), reference),
	}}
}
