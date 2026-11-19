// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sendcloud

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
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {
		APIUser: sdkgo.NewSecretString(testAPIUser), APIKey: sdkgo.NewSecretString(testAPIKey),
	}}
	if scenario.Name == "getAccountBalance-defect" {
		credentials = sdkgo.StaticCredentialProvider[Credentials]{}
	}
	config := Config{BaseURL: endpoint}
	if scenario.Name == "getAccountBalance-oversized" {
		config.MaxResponseBytes = 8
	}
	client, err := New(config, credentials, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getAccountBalance": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetAccountBalance(), testConnection),
	}}
}
