// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package nimble

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/api/v1"
const conformanceFixturePathPrefix = "/api/v1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "nimble", Name: "nimble"}
	credentials := sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString("test-token")}}
	if scenario.Name == "listContactIDs-defect" {
		credentials = sdkgo.StaticCredentialProvider[Credentials]{}
	}
	client, err := New(Config{BaseURL: endpoint}, credentials, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listContactIDs": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContactIDs(), reference),
		"getContact":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetContact(), reference),
	}}
}
