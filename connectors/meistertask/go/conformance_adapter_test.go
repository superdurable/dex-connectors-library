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
		"listTaskChecklists": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTaskChecklists(), reference),
		"getChecklist": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetChecklist(), reference),
		"updateChecklist": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateChecklist(), reference, scenario.CallID),
		"deleteChecklist": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteChecklist(), reference, scenario.CallID),
		"createTaskChecklist": newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateTaskChecklist(), reference, scenario.CallID),
		"listProjectLabels": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListProjectLabels(), reference),
		"listTaskLabels": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTaskLabels(), reference),
		"removeTaskLabel": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.RemoveTaskLabel(), reference, scenario.CallID),
		"addTaskLabel": newConformanceMutationInvocation(t, scenario.Input, createContext, client.AddTaskLabel(), reference, scenario.CallID),
		"listComments": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListComments(), reference),
		"getComment": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetComment(), reference),
		"deleteComment": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteComment(), reference, scenario.CallID),
		"createComment": newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateComment(), reference, scenario.CallID),
		"listSections": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListSections(), reference),
		"getSection": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetSection(), reference),
		"updateSection": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateSection(), reference, scenario.CallID),
		"createSection": newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateSection(), reference, scenario.CallID),
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
