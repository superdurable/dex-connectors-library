// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package juhe

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
	credentials := Credentials{APIKey: sdkgo.NewSecretString(testAPIKey)}

	config := Config{BaseURL: endpoint}
	if scenario.Name == "getCurrentWeather-oversized" || strings.HasSuffix(scenario.Name, "-smallLimit") {
		config.MaxResponseBytes = 16
	}
	if scenario.Name == "getCurrentWeather-missingCredential" {
		credentials.APIKey = sdkgo.NewSecretString("")
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getCurrentWeather": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCurrentWeather(), testConnection),
	}}
}
