// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package emq

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/public_api/v1"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	config := Config{PlatformEndpoint: endpoint}
	if scenario.Name == "listDeployments-oversize" {
		config.MaxResponseBytes = 64
	}
	credentials := Credentials{PlatformAPIKey: sdkgo.NewSecretString("test-platform-key"), PlatformAPISecret: sdkgo.NewSecretString("test-platform-secret")}
	if scenario.Name == "listDeployments-missing-key" {
		credentials = Credentials{}
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listDeployments": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListDeployments(), testConnection),
	}}
}
