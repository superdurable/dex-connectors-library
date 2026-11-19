// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package easemob

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	config := Config{BaseURL: endpoint}
	if scenario.Name == "getUserDetail-oversize-user" {
		config.MaxResponseBytes = 120
	}
	credentials := Credentials{AppKey: "demo#chat", ClientID: "client-id", ClientSecret: sdkgo.NewSecretString("client-secret")}
	if scenario.Name == "getUserDetail-missing-credentials" {
		credentials = Credentials{}
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getUserDetail": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetUserDetail(), testConnection),
	}}
}
