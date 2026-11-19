// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk_test

import (
	"encoding/json"
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
	if len(scenario.PriorHeartbeat) != 0 {
		context.previousHeartbeat = append(json.RawMessage(nil), scenario.PriorHeartbeat...)
		var marker struct {
			Attempt int32 `json:"attempt"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &marker))
		context.attempt = marker.Attempt + 1
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			// A later attempt sees the last heartbeat an earlier attempt stored, as Dex supplies it.
			if context.heartbeatCount > 0 {
				context.previousHeartbeat = append(json.RawMessage(nil), context.recordedHeartbeat...)
			}
			context.recordedHeartbeat = nil
			context.heartbeatCount = 0
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
