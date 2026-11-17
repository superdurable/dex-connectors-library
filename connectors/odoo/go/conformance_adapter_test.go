// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package odoo

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "odoo", Name: "odoo"}
	client, err := New(Config{BaseURL: endpoint, Database: "mycompany"}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("sample-odoo-api-key")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"searchPartners":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.SearchPartners(), reference),
		"listContacts":      newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContacts(), reference),
		"getContact":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetContact(), reference),
		"createContact":     newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateContact(), reference, scenario.CallID),
		"updateContact":     newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateContact(), reference, scenario.CallID),
		"deleteContact":     newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteContact(), reference, scenario.CallID),
		"listOpportunities": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListOpportunities(), reference),
		"getOpportunity":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetOpportunity(), reference),
		"createOpportunity": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateOpportunity(), reference, scenario.CallID),
		"updateOpportunity": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateOpportunity(), reference, scenario.CallID),
		"deleteOpportunity": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteOpportunity(), reference, scenario.CallID),
	}}
}
