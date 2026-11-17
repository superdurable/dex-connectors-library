// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sentry

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {APIKey: sdkgo.NewSecretString("test-secret-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	createContext := &sentryCreateConformanceContext{attempt: 1}
	priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { createContext.heartbeat = value })
	createContext.attempt = priorAttempt + 1
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			createContext.attempt = priorAttempt + int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"listIssues":         newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListIssues(), testConnection),
		"getIssue":           newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetIssue(), testConnection),
		"updateIssueStatus":  newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.UpdateIssueStatus(), testConnection, scenario.CallID),
		"listIssueEvents":    newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListIssueEvents(), testConnection),
		"getIssueEvent":      newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetIssueEvent(), testConnection),
		"listProjects":       newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListProjects(), testConnection),
		"getProject":         newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetProject(), testConnection),
		"listOrganizations":  newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListOrganizations(), testConnection),
		"createOrganization": newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateOrganization(), testConnection, scenario.CallID),
		"getOrganization":    newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetOrganization(), testConnection),
		"updateOrganization": newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.UpdateOrganization(), testConnection, scenario.CallID),
		"updateIssue":        newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.UpdateIssue(), testConnection, scenario.CallID),
		"updateProject":      newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.UpdateProject(), testConnection, scenario.CallID),
		"listProjectEvents":  newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListProjectEvents(), testConnection),
		"getProjectEvent":    newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetProjectEvent(), testConnection),
		"assignIssue":        newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.AssignIssue(), testConnection, scenario.CallID),
		"deleteIssue":        newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.DeleteIssue(), testConnection, scenario.CallID),
		"renameProject":      newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.RenameProject(), testConnection, scenario.CallID),
		"deleteProject":      newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.DeleteProject(), testConnection, scenario.CallID),
		"createProject":      newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateProject(), testConnection, scenario.CallID),
		"createRelease":      newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateRelease(), testConnection, scenario.CallID),
		"getRelease":         newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetRelease(), testConnection),
		"listReleases":       newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListReleases(), testConnection),
		"updateRelease":      newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.UpdateRelease(), testConnection, scenario.CallID),
		"deleteRelease":      newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.DeleteRelease(), testConnection, scenario.CallID),
		"createTeam":         newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateTeam(), testConnection, scenario.CallID),
		"getTeam":            newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetTeam(), testConnection),
		"listTeams":          newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListTeams(), testConnection),
		"updateTeam":         newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.UpdateTeam(), testConnection, scenario.CallID),
		"deleteTeam":         newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.DeleteTeam(), testConnection, scenario.CallID),
	}}
}

type sentryCreateConformanceContext struct {
	providerTestContext
	attempt   int32
	heartbeat json.RawMessage
}

func (context *sentryCreateConformanceContext) Attempt() int32 { return context.attempt }
func (context *sentryCreateConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.heartbeat = encoded
	return nil
}
func (context *sentryCreateConformanceContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, target)
}
