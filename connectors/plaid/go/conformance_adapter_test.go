// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package plaid

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/api"
const conformanceFixturePathPrefix = "/api"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "plaid", Name: "plaid"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {ClientID: sdkgo.NewSecretString("sample-plaid-client-id"), Secret: sdkgo.NewSecretString("sample-plaid-secret"), AccessToken: sdkgo.NewSecretString("sample-plaid-access-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getAccounts": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetAccounts(), reference),
	}}
}
