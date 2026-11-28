// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gitcode

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
	credentials := Credentials{AccessToken: sdkgo.NewSecretString(testAccessToken)}
	configuration := Config{BaseURL: endpoint}
	if scenario.Name == "getRepository-oversized" || scenario.Name == "getRepository-missing-smallLimit" {
		configuration.MaxResponseBytes = 16
	}
	client, err := New(configuration, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getRepository": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetRepository(), testConnection),
	}}
}
