// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bitbucket

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/2.0"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {AccessToken: sdkgo.NewSecretString("test-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"createRepository": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateRepository(), testConnection, scenario.CallID),
		"updateRepository": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateRepository(), testConnection, scenario.CallID),
		"deleteRepository": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteRepository(), testConnection, scenario.CallID),
		"listRepositories":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListRepositories(), testConnection),
		"listPullRequests":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListPullRequests(), testConnection),
		"getRepository":          newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetRepository(), testConnection),
		"getPullRequest":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetPullRequest(), testConnection),
		"updatePullRequestTitle": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdatePullRequestTitle(), testConnection, scenario.CallID),
	}}
}
