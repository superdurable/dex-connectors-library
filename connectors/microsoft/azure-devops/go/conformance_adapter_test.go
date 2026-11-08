// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package azuredevops

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/example"
const conformanceFixturePathPrefix = "/example"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "azure-devops", Name: "azure-devops"}
	credentials := Credentials{PersonalAccessToken: sdkgo.NewSecretString("test-token")}
	if scenario.Name == "listProjects-defect" {
		credentials = Credentials{}
	}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listProjects": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListProjects(), reference),
		"getWorkItem":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetWorkItem(), reference),
	}}
}
