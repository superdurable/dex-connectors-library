// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk/go"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = "/api/v1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client := newDeskClient(t, endpoint, desk.WithHTTPClient(transport))
	context := newDeskDexContext(scenario.Name)
	initialAttempt := int32(1)
	if len(scenario.PriorHeartbeat) > 0 {
		var previous struct {
			Attempt int32           `json:"attempt"`
			Value   json.RawMessage `json:"value"`
		}
		if err := json.Unmarshal(scenario.PriorHeartbeat, &previous); err != nil {
			t.Fatal(err)
		}
		context.previousHeartbeat = previous.Value
		initialAttempt = previous.Attempt + 1
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt == 1 {
			context.attempt = initialAttempt
			return
		}
		context.previousHeartbeat = context.recordedHeartbeat
		context.recordedHeartbeat = nil
		context.attempt = initialAttempt + int32(attempt) - 1
	}, actions: map[string]conformanceOperationInvocation{
		"searchTickets": newConformanceQueryInvocation(t, scenario.Input, context, client.SearchTickets(), deskConnection),
		"getTicket":     newConformanceQueryInvocation(t, scenario.Input, context, client.GetTicket(), deskConnection),
		"createTicket":  newConformanceMutationInvocation(t, scenario.Input, context, client.CreateTicket(), deskConnection, scenario.CallID),
		"updateTicket":  newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateTicket(), deskConnection, scenario.CallID),
		"addComment":    newConformanceMutationInvocation(t, scenario.Input, context, client.AddComment(), deskConnection, scenario.CallID),
	}}
}
