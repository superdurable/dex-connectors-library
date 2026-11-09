// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package elasticsearch

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
	client, err := New(Config{ClusterURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {Username: "dex_reader", Password: sdkgo.NewSecretString("fake-password")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"indexDocument": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.IndexDocument(), testConnection, scenario.CallID),
		"deleteDocument": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteDocument(), testConnection, scenario.CallID),
		"searchDocuments": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.SearchDocuments(), testConnection),
	}}
}
