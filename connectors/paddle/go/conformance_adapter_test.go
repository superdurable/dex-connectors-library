// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paddle

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = "/api"
const conformanceFixturePathPrefix = "/api"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "paddle", Name: "paddle"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("sample-paddle-api-key")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := singledispatchtest.NewContext("conformance-flow", "paddle-customer")
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 { context.AdvanceAttempt() }
	}, actions: map[string]conformanceOperationInvocation{
		"getTransaction": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetTransaction(), reference),
		"listCustomers": newConformanceQueryInvocation(t, scenario.Input, context, client.ListCustomers(), reference),
		"getCustomer": newConformanceQueryInvocation(t, scenario.Input, context, client.GetCustomer(), reference),
		"createCustomer": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateCustomer(), reference, scenario.CallID),
		"updateCustomer": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateCustomer(), reference, scenario.CallID),
		"archiveCustomer": newConformanceMutationInvocation(t, scenario.Input, context, client.ArchiveCustomer(), reference, scenario.CallID),
	}}
}
