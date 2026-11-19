// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zentao

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = "/zentao"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{Token: sdkgo.NewSecretString("secretToken")}
	if scenario.Name == "listProducts-defect" {
		credentials = Credentials{Token: sdkgo.NewSecretString("bad token")}
	}
	configuration := Config{BaseURL: endpoint + "/zentao"}
	if scenario.Name == "listProducts-oversized" {
		configuration.MaxResponseBytes = 16
	}
	client, err := New(configuration, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listProducts": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListProducts(), testConnection),
	}}
}
