// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oraclenetsuite

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
	reference := sdkgo.ConnectionRef{Provider: "oracle-netsuite", Name: "oracle-netsuite"}
	client, err := New(Config{BaseURL: endpoint, AuthorizationEndpoint: endpoint + "/authorize", TokenEndpoint: endpoint + "/token"}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {
			OAuthClientID: "synthetic-client-id", OAuthClientSecret: sdkgo.NewSecretString("synthetic-client-secret"),
			AccessToken: sdkgo.NewSecretString("test-oracle-netsuite-token"), RefreshToken: sdkgo.NewSecretString("synthetic-refresh-token"),
		},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := singledispatchtest.NewContext("conformance-flow", "netsuite-customer")
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"getCustomer":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCustomer(), reference),
		"listCustomers":  newConformanceQueryInvocation(t, scenario.Input, context, client.ListCustomers(), reference),
		"createCustomer": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateCustomer(), reference, scenario.CallID),
		"updateCustomer": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateCustomer(), reference, scenario.CallID),
		"deleteCustomer": newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteCustomer(), reference, scenario.CallID),
		"getVendor":      newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetVendor(), reference),
		"listVendors":    newConformanceQueryInvocation(t, scenario.Input, context, client.ListVendors(), reference),
		"createVendor":   newConformanceMutationInvocation(t, scenario.Input, context, client.CreateVendor(), reference, scenario.CallID),
		"updateVendor":   newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateVendor(), reference, scenario.CallID),
		"deleteVendor":   newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteVendor(), reference, scenario.CallID),
	}}
}
