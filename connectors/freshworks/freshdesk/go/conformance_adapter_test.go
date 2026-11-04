// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk/go"
)

const conformanceEndpointPath = "/api/v2"
const conformanceFixturePathPrefix = "/api/v2"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := freshdesk.New(freshdesk.Config{Domain: testDomain}, testCredentialProvider(), freshdesk.WithAPIBaseURL(endpoint), freshdesk.WithHTTPClient(transport))
	require.NoError(t, err)
	context := newFreshdeskDexContext("conformance")
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.previousHeartbeat = context.recordedHeartbeat
			context.recordedHeartbeat = nil
			context.attempt = int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"searchTickets": newConformanceQueryInvocation(t, scenario.Input, context, client.SearchTickets(), freshdeskConnection),
		"getTicket":     newConformanceQueryInvocation(t, scenario.Input, context, client.GetTicket(), freshdeskConnection),
		"createTicket":  newConformanceMutationInvocation(t, scenario.Input, context, client.CreateTicket(), freshdeskConnection, scenario.CallID),
		"updateTicket":  newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateTicket(), freshdeskConnection, scenario.CallID),
		"addNote":       newConformanceMutationInvocation(t, scenario.Input, context, client.AddNote(), freshdeskConnection, scenario.CallID),
	}}
}
