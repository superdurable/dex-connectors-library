// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dataeyes

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, _ string, transport *http.Client) conformanceInvoker {
	config := Config{}
	if scenario.Name == "readWebPage-oversize" {
		config.MaxResponseBytes = 16
	}
	credentials := Credentials{APIKey: sdkgo.NewSecretString("test-secret")}
	if scenario.Name == "readWebPage-missing-key" {
		credentials = Credentials{}
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"readWebPage": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ReadWebPage(), testConnection),
	}}
}
