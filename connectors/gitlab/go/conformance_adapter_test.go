// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gitlab

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
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {AccessToken: sdkgo.NewSecretString("test-accesstoken")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	mutationContext := singledispatchtest.NewContext("gitlab-provider-test", "mutation")
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			mutationContext.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"getProject":         newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.GetProject(), testConnection),
		"listIssues":         newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.ListIssues(), testConnection),
		"updateIssueLabels":  newConformanceMutationInvocation(t, scenario.Input, testContext{}, client.UpdateIssueLabels(), testConnection, scenario.CallID),
		"createIssue":        newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.CreateIssue(), testConnection, scenario.CallID),
		"createIssueComment": newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.CreateIssueComment(), testConnection, scenario.CallID),
		"getIssue":           newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.GetIssue(), testConnection),
		"editIssue":          newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.EditIssue(), testConnection, scenario.CallID),
		"lockIssue":          newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.LockIssue(), testConnection, scenario.CallID),
		"deleteIssue":        newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.DeleteIssue(), testConnection, scenario.CallID),
		"createRelease":      newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.CreateRelease(), testConnection, scenario.CallID),
		"getRelease":         newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.GetRelease(), testConnection),
		"listReleases":       newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.ListReleases(), testConnection),
		"updateRelease":      newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.UpdateRelease(), testConnection, scenario.CallID),
		"deleteRelease":      newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.DeleteRelease(), testConnection, scenario.CallID),
		"listRepositoryTree": newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.ListRepositoryTree(), testConnection),
		"listUserProjects":   newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.ListUserProjects(), testConnection),
	}}
}
