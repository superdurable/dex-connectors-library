// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/gorgias/go"
)

const conformanceEndpointPath = "/api"
const conformanceFixturePathPrefix = "/api"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := gorgias.New(gorgias.Config{Domain: testDomain}, testCredentialProvider(), gorgias.WithAPIBaseURL(endpoint), gorgias.WithHTTPClient(transport))
	require.NoError(t, err)
	context := newGorgiasDexContext("conformance")
	context.previousHeartbeat = scenario.PriorHeartbeat
	attemptOffset := 0
	if len(scenario.PriorHeartbeat) != 0 {
		var marker struct {
			Attempt int `json:"attempt"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &marker))
		attemptOffset = marker.Attempt
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 && context.recordedHeartbeat != nil {
			context.previousHeartbeat = context.recordedHeartbeat
		}
		context.attempt = int32(attempt + attemptOffset)
	}, actions: map[string]conformanceOperationInvocation{
		"searchTickets":       newConformanceQueryInvocation(t, scenario.Input, context, client.SearchTickets(), gorgiasConnection),
		"getTicket":           newConformanceQueryInvocation(t, scenario.Input, context, client.GetTicket(), gorgiasConnection),
		"findCustomerByEmail": newConformanceQueryInvocation(t, scenario.Input, context, client.FindCustomerByEmail(), gorgiasConnection),
		"createTicket":        newConformanceMutationInvocation(t, scenario.Input, context, client.CreateTicket(), gorgiasConnection, scenario.CallID),
		"updateTicket":        newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateTicket(), gorgiasConnection, scenario.CallID),
		"addNote":             newConformanceMutationInvocation(t, scenario.Input, context, client.AddNote(), gorgiasConnection, scenario.CallID),
	}}
}
