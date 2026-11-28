// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tripo

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
	credentials := Credentials{APIKey: sdkgo.NewSecretString("test-api-key")}
	if scenario.Name == "getAccountBalance-defect" {
		credentials = Credentials{}
	}
	config := Config{BaseURL: endpoint}
	if scenario.Name == "getAccountBalance-oversize" {
		config.MaxResponseBytes = 8
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getAccountBalance": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetAccountBalance(), testConnection),
	}}
}
