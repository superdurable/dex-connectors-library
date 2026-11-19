// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package todoist

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/api/v1"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {APIToken: sdkgo.NewSecretString(testToken)},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := newAttemptContext()
	initialAttempt := int32(1)
	if len(scenario.PriorHeartbeat) > 0 {
		var prior struct {
			Attempt int32 `json:"attempt"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &prior))
		initialAttempt = prior.Attempt + 1
		// An SDK fence marker is restored as Dex persisted it.
		context.previousHeartbeat = scenario.PriorHeartbeat
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt == 1 {
			context.attempt = initialAttempt
			return
		}
		context.nextAttempt()
	}, actions: map[string]conformanceOperationInvocation{
		"getTask":           newConformanceQueryInvocation(t, scenario.Input, context, client.GetTask(), testConnection),
		"updateTaskContent": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateTask(), testConnection, scenario.CallID),
		"listTasks":         newConformanceQueryInvocation(t, scenario.Input, context, client.ListTasks(), testConnection),
		"createTask":        newConformanceMutationInvocation(t, scenario.Input, context, client.CreateTask(), testConnection, scenario.CallID),
		"deleteTask":        newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteTask(), testConnection, scenario.CallID),
	}}
}
