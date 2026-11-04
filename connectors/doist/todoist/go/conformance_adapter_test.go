// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package todoist

import (
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
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getTask": newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.GetTask(), testConnection),
		"updateTaskContent": newConformanceMutationInvocation(t, scenario.Input, providerContext{}, client.UpdateTask(), testConnection, scenario.CallID),
		"listTasks": newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.ListTasks(), testConnection),
		"createTask": newConformanceMutationInvocation(t, scenario.Input, providerContext{}, client.CreateTask(), testConnection, scenario.CallID),
		"deleteTask": newConformanceMutationInvocation(t, scenario.Input, providerContext{}, client.DeleteTask(), testConnection, scenario.CallID),
	}}
}
