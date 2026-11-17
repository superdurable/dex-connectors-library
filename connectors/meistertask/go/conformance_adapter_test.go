// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meistertask

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "meistertask", Name: "meistertask"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIToken: sdkgo.NewSecretString("sample-meistertask-api-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	createContext := singledispatchtest.NewContext("meistertask-conformance", "create-task")
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			createContext.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"listProjects": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListProjects(), reference),
		"getProject": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetProject(), reference),
		"updateProject": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateProject(), reference, scenario.CallID),
		"createProject": newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateProject(), reference, scenario.CallID),
		"listTasks":  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTasks(), reference),
		"getTask":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetTask(), reference),
		"updateTask": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateTask(), reference, scenario.CallID),
		"createTask": newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateTask(), reference, scenario.CallID),
	}}
}
