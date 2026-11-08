// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package constantcontact

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v3"
const conformanceFixturePathPrefix = "/v3"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "constantcontact", Name: "constantcontact"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {
			OAuthClientID: "app-id", OAuthClientSecret: sdkgo.NewSecretString("app-secret"),
			AccessToken: sdkgo.NewSecretString("sample-access-token"), RefreshToken: sdkgo.NewSecretString("sample-refresh-token"),
		},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getContact": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetContact(), reference),
	}}
}
