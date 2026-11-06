// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira-service-management/go"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client := newTestClient(t, endpoint, jiraservicemanagement.WithHTTPClient(transport))
	context := newTestDexContext("conformance")
	previousAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) {
		context.previousHeartbeat = append(json.RawMessage(nil), value...)
	})
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.previousHeartbeat = context.recordedHeartbeat
			context.recordedHeartbeat = nil
		}
		context.attempt = previousAttempt + int32(attempt)
	}, actions: map[string]conformanceOperationInvocation{
		"findCustomerByEmail": newConformanceQueryInvocation(t, scenario.Input, context, client.FindCustomerByEmail(), jsmConnection),
		"searchTickets": newConformanceQueryInvocation(t, scenario.Input, context, client.SearchTickets(), jsmConnection),
		"getTicket": newConformanceQueryInvocation(t, scenario.Input, context, client.GetTicket(), jsmConnection),
		"createTicket": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateTicket(), jsmConnection, scenario.CallID),
		"updateTicket": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateTicket(), jsmConnection, scenario.CallID),
		"transitionTicket": newConformanceMutationInvocation(t, scenario.Input, context, client.TransitionTicket(), jsmConnection, scenario.CallID),
		"addComment": newConformanceMutationInvocation(t, scenario.Input, context, client.AddComment(), jsmConnection, scenario.CallID),
	}}
}
