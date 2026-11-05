// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package github_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/github/go"
	"github.com/superdurable/dex-connectors-library/connectors/github/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	accessToken := "one-use-token"
	if scenario.Name == "getAuthenticatedProfile-defect" {
		accessToken = ""
	}
	client, err := github.New(github.Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[github.Credentials]{
		githubConnection: {AccessToken: sdkgo.NewSecretString(accessToken)},
	}, github.WithHTTPClient(transport))
	require.NoError(t, err)
	context := testsupport.NewDexContext("github-conformance", "github-step")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getAuthenticatedProfile": newConformanceQueryInvocation(t, scenario.Input, context, client.GetAuthenticatedProfile(), githubConnection),
		"listPublicRepositories":  newConformanceQueryInvocation(t, scenario.Input, context, client.ListPublicRepositories(), githubConnection),
		"listMergedPullRequests":  newConformanceQueryInvocation(t, scenario.Input, context, client.ListMergedPullRequests(), githubConnection),
		"listPullRequestFiles":    newConformanceQueryInvocation(t, scenario.Input, context, client.ListPullRequestFiles(), githubConnection),
		"listReleases":            newConformanceQueryInvocation(t, scenario.Input, context, client.ListReleases(), githubConnection),
		"listCommits":             newConformanceQueryInvocation(t, scenario.Input, context, client.ListCommits(), githubConnection),
	}}
}
