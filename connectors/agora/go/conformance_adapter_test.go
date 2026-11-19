// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package agora

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
	credentials := sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {
			AppID: "1234567890abcdef1234567890abcdef",
			CustomerID: "customer-id",
			CustomerSecret: sdkgo.NewSecretString("customer-secret"),
		},
	}
	if scenario.Name == "listRunningAgents-defect" {
		credentials = sdkgo.StaticCredentialProvider[Credentials]{}
	}
	client, err := New(Config{BaseURL: endpoint}, credentials, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listRunningAgents": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListRunningAgents(), testConnection),
	}}
}
