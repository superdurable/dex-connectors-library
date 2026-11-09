// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gusto

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
	reference := sdkgo.ConnectionRef{Provider: "gusto", Name: "gusto"}
	credentials := Credentials{
		OAuthClientID: "synthetic-client-id", OAuthClientSecret: sdkgo.NewSecretString("synthetic-client-secret"),
		AccessToken: sdkgo.NewSecretString("test-gusto-token"), RefreshToken: sdkgo.NewSecretString("synthetic-refresh-token"),
	}
	client, err := New(Config{BaseURL: endpoint, RedirectURI: "https://dex.example/callback"}, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getEmployee": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetEmployee(), reference),
	}}
}
