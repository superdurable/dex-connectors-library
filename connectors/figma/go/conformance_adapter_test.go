// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package figma

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v1"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	connection := sdkgo.ConnectionRef{Provider: "figma", Name: "figma"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		connection: {APIKey: sdkgo.NewSecretString(testAPIKey)},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getFile":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetFile(), connection),
		"getFileComments": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetFileComments(), connection),
	}}
}
