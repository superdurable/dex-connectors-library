// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wordpress

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
	client, err := New(Config{SiteURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {Username: "editor", ApplicationPassword: sdkgo.NewSecretString("synthetic test password")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := singledispatchtest.NewContext("wordpress-conformance", scenario.Name)
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"createPost": newConformanceMutationInvocation(t, scenario.Input, context, client.CreatePost(), testConnection, scenario.CallID),
		"getPost":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetPost(), testConnection),
		"listPosts":  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListPosts(), testConnection),
		"updatePost": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdatePost(), testConnection, scenario.CallID),
		"deletePost": newConformanceMutationInvocation(t, scenario.Input, context, client.DeletePost(), testConnection, scenario.CallID),
		"createPage": newConformanceMutationInvocation(t, scenario.Input, context, client.CreatePage(), testConnection, scenario.CallID),
		"getPage": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetPage(), testConnection),
		"listPages": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListPages(), testConnection),
		"updatePage": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdatePage(), testConnection, scenario.CallID),
		"deletePage": newConformanceMutationInvocation(t, scenario.Input, context, client.DeletePage(), testConnection, scenario.CallID),
		"getUser": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetUser(), testConnection),
		"listUsers": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListUsers(), testConnection),
		"updateUser": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateUser(), testConnection, scenario.CallID),
	}}
}
