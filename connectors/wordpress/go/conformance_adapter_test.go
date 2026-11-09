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
	}}
}
