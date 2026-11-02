// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package todoist

import (
	"net/http"
	"testing"
)

const conformanceEndpointPath = "/api/v1"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client := newTestClient(t, endpoint, WithHTTPClient(transport))
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getTask":           newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.GetTask(), testConnection),
		"updateTaskContent": newConformanceMutationInvocation(t, scenario.Input, providerContext{}, client.UpdateTask(), testConnection, scenario.CallID),
		"listTasks":         newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.ListTasks(), testConnection),
		"createTask":        newConformanceMutationInvocation(t, scenario.Input, providerContext{}, client.CreateTask(), testConnection, scenario.CallID),
		"deleteTask":        newConformanceMutationInvocation(t, scenario.Input, providerContext{}, client.DeleteTask(), testConnection, scenario.CallID),
	}}
}
