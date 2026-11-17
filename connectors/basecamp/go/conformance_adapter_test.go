// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package basecamp

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "basecamp", Name: "basecamp"}
	credentials := Credentials{
		OAuthClientID: "synthetic-client-id", OAuthClientSecret: sdkgo.NewSecretString("synthetic-client-secret"),
		AccessToken: sdkgo.NewSecretString("test-basecamp-token"), RefreshToken: sdkgo.NewSecretString("synthetic-refresh-token"),
	}
	config := Config{BaseURL: endpoint, APIBaseURL: endpoint}
	if strings.HasPrefix(scenario.Name, "listAccounts-oversized") {
		config.MaxResponseBytes = 16
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	fenceContext := singledispatchtest.NewContext("basecamp-conformance", "create-project")
	if len(scenario.PriorHeartbeat) != 0 {
		var prior struct {
			Attempt int32           `json:"attempt"`
			Value   json.RawMessage `json:"value"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &prior))
		for attempt := int32(0); attempt < prior.Attempt; attempt++ {
			fenceContext.AdvanceAttempt()
		}
		require.NoError(t, fenceContext.SetPersistedCheckpoint(prior.Value))
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			fenceContext.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"listAccounts":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListAccounts(), reference),
		"listProjects":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListProjects(), reference),
		"getProject":          newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetProject(), reference),
		"listTodos":           newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTodos(), reference),
		"getTodo":             newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetTodo(), reference),
		"createProject":       newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.CreateProject(), reference, scenario.CallID),
		"updateProject":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateProject(), reference, scenario.CallID),
		"archiveProject":      newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.ArchiveProject(), reference, scenario.CallID),
		"unarchiveProject":    newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UnarchiveProject(), reference, scenario.CallID),
		"trashProject":        newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.TrashProject(), reference, scenario.CallID),
		"createTodo":          newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.CreateTodo(), reference, scenario.CallID),
		"updateTodo":          newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateTodo(), reference, scenario.CallID),
		"completeTodo":        newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CompleteTodo(), reference, scenario.CallID),
		"uncompleteTodo":      newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UncompleteTodo(), reference, scenario.CallID),
		"trashTodo":           newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.TrashTodo(), reference, scenario.CallID),
		"repositionTodo":      newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.RepositionTodo(), reference, scenario.CallID),
		"listTodoLists":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTodoLists(), reference),
		"getTodoList":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetTodoList(), reference),
		"createTodoList":      newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.CreateTodoList(), reference, scenario.CallID),
		"updateTodoList":      newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateTodoList(), reference, scenario.CallID),
		"repositionTodoList":  newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.RepositionTodoList(), reference, scenario.CallID),
		"trashTodoList":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.TrashTodoList(), reference, scenario.CallID),
		"listPeople":          newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListPeople(), reference),
		"listProjectPeople":   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListProjectPeople(), reference),
		"getPerson":           newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetPerson(), reference),
		"grantProjectAccess":  newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.GrantProjectAccess(), reference, scenario.CallID),
		"revokeProjectAccess": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.RevokeProjectAccess(), reference, scenario.CallID),
		"enrollPerson":        newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.EnrollPerson(), reference, scenario.CallID),
		"listMessages":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListMessages(), reference),
		"getMessage":          newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetMessage(), reference),
		"createMessage":       newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.CreateMessage(), reference, scenario.CallID),
		"updateMessage":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateMessage(), reference, scenario.CallID),
		"publishMessage":      newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.PublishMessage(), reference, scenario.CallID),
		"pinMessage":          newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.PinMessage(), reference, scenario.CallID),
		"unpinMessage":        newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UnpinMessage(), reference, scenario.CallID),
		"trashMessage":        newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.TrashMessage(), reference, scenario.CallID),
	}}
}
