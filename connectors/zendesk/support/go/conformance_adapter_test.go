// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go"
)

const conformanceEndpointPath = "/api/v2"
const conformanceFixturePathPrefix = "/api/v2"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := support.New(support.Config{Subdomain: testSubdomain}, testCredentialProvider(),
		support.WithAPIBaseURL(endpoint), support.WithHTTPClient(transport))
	require.NoError(t, err)
	context := newZendeskDexContext("conformance")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"searchTickets": newConformanceQueryInvocation(t, scenario.Input, context, client.SearchTickets(), zendeskConnection),
		"getTicket":     newConformanceQueryInvocation(t, scenario.Input, context, client.GetTicket(), zendeskConnection),
		"createTicket":  newConformanceMutationInvocation(t, scenario.Input, context, client.CreateTicket(), zendeskConnection, scenario.CallID),
		"updateTicket":  newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateTicket(), zendeskConnection, scenario.CallID),
	}}
}
