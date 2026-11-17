// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package vercel

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net/http"
	"testing"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	token := "test-accesstoken"
	if scenario.Name == "listDeployments-defect" {
		token = ""
	}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessToken: sdkgo.NewSecretString(token)}}, WithHTTPClient(transport))
	require.NoError(t, err)
	ctx := &projectTestContext{}
	if len(scenario.PriorHeartbeat) > 0 {
		ctx.heartbeat = append(json.RawMessage(nil), scenario.PriorHeartbeat...)
		var marker struct {
			Attempt int32 `json:"attempt"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &marker))
		ctx.attempt = marker.Attempt + 1
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			ctx.attempt = int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"listDeployments":     newConformanceQueryInvocation(t, scenario.Input, ctx, client.ListDeployments(), testConnection),
		"getDeployment":       newConformanceQueryInvocation(t, scenario.Input, ctx, client.GetDeployment(), testConnection),
		"cancelDeployment":    newConformanceMutationInvocation(t, scenario.Input, ctx, client.CancelDeployment(), testConnection, scenario.CallID),
		"deleteDeployment":    newConformanceMutationInvocation(t, scenario.Input, ctx, client.DeleteDeployment(), testConnection, scenario.CallID),
		"listProjects":        newConformanceQueryInvocation(t, scenario.Input, ctx, client.ListProjects(), testConnection),
		"getProject":          newConformanceQueryInvocation(t, scenario.Input, ctx, client.GetProject(), testConnection),
		"createProject":       newConformanceMutationInvocation(t, scenario.Input, ctx, client.CreateProject(), testConnection, scenario.CallID),
		"updateProject":       newConformanceMutationInvocation(t, scenario.Input, ctx, client.UpdateProject(), testConnection, scenario.CallID),
		"deleteProject":       newConformanceMutationInvocation(t, scenario.Input, ctx, client.DeleteProject(), testConnection, scenario.CallID),
		"listProjectDomains":  newConformanceQueryInvocation(t, scenario.Input, ctx, client.ListProjectDomains(), testConnection),
		"getProjectDomain":    newConformanceQueryInvocation(t, scenario.Input, ctx, client.GetProjectDomain(), testConnection),
		"addProjectDomain":    newConformanceMutationInvocation(t, scenario.Input, ctx, client.AddProjectDomain(), testConnection, scenario.CallID),
		"updateProjectDomain": newConformanceMutationInvocation(t, scenario.Input, ctx, client.UpdateProjectDomain(), testConnection, scenario.CallID),
		"removeProjectDomain": newConformanceMutationInvocation(t, scenario.Input, ctx, client.RemoveProjectDomain(), testConnection, scenario.CallID),
		"verifyProjectDomain": newConformanceMutationInvocation(t, scenario.Input, ctx, client.VerifyProjectDomain(), testConnection, scenario.CallID),
	}}
}
