// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oraclenetsuite

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
	reference := sdkgo.ConnectionRef{Provider: "oracle-netsuite", Name: "oracle-netsuite"}
	client, err := New(Config{BaseURL: endpoint, AuthorizationEndpoint: endpoint + "/authorize", TokenEndpoint: endpoint + "/token"}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {
			OAuthClientID: "synthetic-client-id", OAuthClientSecret: sdkgo.NewSecretString("synthetic-client-secret"),
			AccessToken: sdkgo.NewSecretString("test-oracle-netsuite-token"), RefreshToken: sdkgo.NewSecretString("synthetic-refresh-token"),
		},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getCustomer": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCustomer(), reference),
	}}
}
