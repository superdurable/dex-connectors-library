// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package seniverse

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net/http"
	"strings"
	"testing"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{PrivateKey: sdkgo.NewSecretString("private-test-key")}
	if scenario.Name == "getCurrentWeather-missingCredential" {
		credentials.PrivateKey = sdkgo.NewSecretString("")
	}
	config := Config{BaseURL: endpoint}
	if strings.HasSuffix(scenario.Name, "-smallLimit") {
		config.MaxResponseBytes = 16
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getCurrentWeather": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCurrentWeather(), testConnection),
	}}
}
