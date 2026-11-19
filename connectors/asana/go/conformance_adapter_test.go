// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/asana/go"
)

const conformanceEndpointPath = "/api/1.0"
const conformanceFixturePathPrefix = "/api/1.0"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client := newAsanaClient(t, endpoint, asana.WithHTTPClient(transport))
	var prior json.RawMessage
	if len(scenario.PriorHeartbeat) > 0 {
		var wrapped struct {
			Value json.RawMessage `json:"value"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &wrapped))
		if len(wrapped.Value) > 0 {
			prior = wrapped.Value
		} else {
			// An SDK fence marker is restored as Dex persisted it.
			prior = scenario.PriorHeartbeat
		}
	}
	context := newReplayAsanaDexContext("conformance", 1, prior)
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { context.attempt = int32(attempt) + initialConformanceAttempt(t, scenario) }, actions: map[string]conformanceOperationInvocation{
		"listTasks":  newConformanceQueryInvocation(t, scenario.Input, context, client.ListTasks(), asanaConnection),
		"getTask":    newConformanceQueryInvocation(t, scenario.Input, context, client.GetTask(), asanaConnection),
		"createTask": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateTask(), asanaConnection, scenario.CallID),
		"updateTask": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateTask(), asanaConnection, scenario.CallID),
		"addComment": newConformanceMutationInvocation(t, scenario.Input, context, client.AddComment(), asanaConnection, scenario.CallID),
	}}
}

func initialConformanceAttempt(t *testing.T, scenario conformanceScenario) int32 {
	if len(scenario.PriorHeartbeat) == 0 {
		return 0
	}
	var marker struct {
		Attempt int32 `json:"attempt"`
	}
	require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &marker))
	return marker.Attempt
}
