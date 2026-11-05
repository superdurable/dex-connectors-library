// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	jiraservicemanagement "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira-service-management/go"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client := newTestClient(t, endpoint, jiraservicemanagement.WithHTTPClient(transport))
	context := newTestDexContext("conformance")
	initialAttempt := int32(1)
	if len(scenario.PriorHeartbeat) > 0 {
		var marker struct {
			CallID string `json:"callId"`
			Attempt int32 `json:"attempt"`
			State string `json:"state"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &marker))
		initialAttempt = marker.Attempt + 1
		var err error
		context.previousHeartbeat, err = json.Marshal(map[string]any{
			"jiraServiceManagementDispatchedCallId": marker.CallID,
			"attempt": marker.Attempt,
			"canResend": marker.State == "canResend" || marker.State == "notSent",
		})
		require.NoError(t, err)
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
		"findCustomerByEmail": newConformanceQueryInvocation(t, scenario.Input, context, client.FindCustomerByEmail(), jsmConnection),
		"searchTickets": newConformanceQueryInvocation(t, scenario.Input, context, client.SearchTickets(), jsmConnection),
		"getTicket": newConformanceQueryInvocation(t, scenario.Input, context, client.GetTicket(), jsmConnection),
		"createTicket": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateTicket(), jsmConnection, scenario.CallID),
		"updateTicket": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateTicket(), jsmConnection, scenario.CallID),
		"transitionTicket": newConformanceMutationInvocation(t, scenario.Input, context, client.TransitionTicket(), jsmConnection, scenario.CallID),
		"addComment": newConformanceMutationInvocation(t, scenario.Input, context, client.AddComment(), jsmConnection, scenario.CallID),
	}}
}
