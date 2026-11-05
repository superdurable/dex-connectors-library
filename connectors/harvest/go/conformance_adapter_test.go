// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package harvest

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
	connection := sdkgo.ConnectionRef{Provider: "harvest", Name: "harvest"}
	client, err := New(Config{BaseURL: endpoint, AccountID: "123"}, sdkgo.StaticCredentialProvider[Credentials]{connection: {APIKey: sdkgo.NewSecretString("test-secret-key")}}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listProjects":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListProjects(), connection),
		"listTimeEntries": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTimeEntries(), connection),
	}}
}
