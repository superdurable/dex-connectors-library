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
		"listAccounts":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListAccounts(), reference),
		"listProjects":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListProjects(), reference),
		"getProject":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetProject(), reference),
		"listTodos":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTodos(), reference),
		"getTodo":          newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetTodo(), reference),
		"createProject":    newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.CreateProject(), reference, scenario.CallID),
		"updateProject":    newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateProject(), reference, scenario.CallID),
		"archiveProject":   newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.ArchiveProject(), reference, scenario.CallID),
		"unarchiveProject": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UnarchiveProject(), reference, scenario.CallID),
		"trashProject":     newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.TrashProject(), reference, scenario.CallID),
	}}
}
