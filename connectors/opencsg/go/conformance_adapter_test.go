// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package opencsg

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("test-secret")}
	switch scenario.Name {
	case "listGatewayModels-defect":
		credentials = Credentials{}
	case "listGatewayModels-longKey":
		credentials.APIKey = sdkgo.NewSecretString(strings.Repeat("x", 1024))
	case "listGatewayModels-nonAsciiKey":
		credentials.APIKey = sdkgo.NewSecretString("密钥")
	}
	config := Config{}
	if scenario.Name == "listGatewayModels-oversized" || strings.HasSuffix(scenario.Name, "-smallLimit") {
		config.MaxResponseBytes = 16
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listGatewayModels": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListGatewayModels(), testConnection),
	}}
}
