// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bitbucket

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = "/2.0"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {AccessToken: sdkgo.NewSecretString("test-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	fenceContext := singledispatchtest.NewContext("bitbucket-conformance", "create-pull-request")
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
		"createPullRequest":      newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.CreatePullRequest(), testConnection, scenario.CallID),
		"mergePullRequest":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.MergePullRequest(), testConnection, scenario.CallID),
		"declinePullRequest":     newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeclinePullRequest(), testConnection, scenario.CallID),
		"createRepository":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateRepository(), testConnection, scenario.CallID),
		"updateRepository":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateRepository(), testConnection, scenario.CallID),
		"deleteRepository":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteRepository(), testConnection, scenario.CallID),
		"listRepositories":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListRepositories(), testConnection),
		"listPullRequests":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListPullRequests(), testConnection),
		"getRepository":          newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetRepository(), testConnection),
		"getPullRequest":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetPullRequest(), testConnection),
		"updatePullRequestTitle": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdatePullRequestTitle(), testConnection, scenario.CallID),
	}}
}
