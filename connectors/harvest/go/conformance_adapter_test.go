// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package harvest

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v2"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := New(Config{BaseURL: endpoint, AccountID: "123"}, sdkgo.StaticCredentialProvider[Credentials]{
		sdkgo.ConnectionRef{Provider: "harvest", Name: "harvest"}: {APIKey: sdkgo.NewSecretString(testAPIKey)},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	connection := sdkgo.ConnectionRef{Provider: "harvest", Name: "harvest"}
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listProjects":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListProjects(), connection),
		"listTimeEntries": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTimeEntries(), connection),
	}}
}
