// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package youzan

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
	credentials := Credentials{AccessToken: sdkgo.NewSecretString("sample-test-token")}
	if scenario.Name == "getShopBasic-defect" {
		credentials = Credentials{}
	}
	config := Config{BaseURL: endpoint}
	if scenario.Name == "getShopBasic-oversized" {
		config.MaxResponseBytes = 16
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getShopBasic": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetShopBasic(), testConnection),
	}}
}
