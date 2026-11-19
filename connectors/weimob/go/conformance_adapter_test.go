// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package weimob

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
	credentials := Credentials{ClientID: "example-client-id", ClientSecret: sdkgo.NewSecretString("REDACTED_CLIENT_SECRET"), ShopID: "400019"}
	if scenario.Name == "getStoreBasic-defect" {
		credentials.ShopID = "bad-shop"
	}
	configuration := Config{BaseURL: endpoint}
	if scenario.Name == "getStoreBasic-oversized" {
		configuration.MaxResponseBytes = 5
	}
	client, err := New(configuration, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getStoreBasic": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetStoreBasic(), testConnection),
	}}
}
