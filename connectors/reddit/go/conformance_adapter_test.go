// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reddit

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "reddit", Name: "reddit"}
	credentials := Credentials{
		OAuthClientID: "synthetic-client-id", OAuthClientSecret: sdkgo.NewSecretString("synthetic-client-secret"),
		AccessToken: sdkgo.NewSecretString("test-reddit-token"), RefreshToken: sdkgo.NewSecretString("synthetic-refresh-token"),
	}
	client, err := New(Config{BaseURL: endpoint, RedditUsername: "example"}, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getPost":            newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetPost(), reference),
		"listSubredditPosts": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListSubredditPosts(), reference),
		"searchPosts":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.SearchPosts(), reference),
		"getUser":            newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetUser(), reference),
		"getSubreddit":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetSubreddit(), reference),
		"searchSubreddits":   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.SearchSubreddits(), reference),
		"listPostComments":   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListPostComments(), reference),
		"listSubreddits":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListSubreddits(), reference),
		"getSubredditRules":  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetSubredditRules(), reference),
	}}
}
