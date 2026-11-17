// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package apizero

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, _ string, transport *http.Client) conformanceInvoker {
	t.Helper()
	config := Config{}
	if scenario.Name == "getAccountInfo-oversized" {
		config.MaxResponseBytes = 16
	}
	apiKey := "sk_live_test-secret"
	if scenario.Name == "getAccountInfo-defect" {
		apiKey = "invalid"
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {APIKey: sdkgo.NewSecretString(apiKey)},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getAccountInfo": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetAccountInfo(), testConnection),
	}}
}
