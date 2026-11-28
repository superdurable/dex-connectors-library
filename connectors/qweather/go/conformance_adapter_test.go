// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qweather

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
	credentials := Credentials{APIHost: sdkgo.NewSecretString("abc123.xy.qweatherapi.com"), APIKey: sdkgo.NewSecretString("private-test-key")}
	if scenario.Name == "getCurrentWeather-invalidHost" {
		credentials.APIHost = sdkgo.NewSecretString("spoof.qweatherapi.com.example")
	}
	if scenario.Name == "getCurrentWeather-missingCredential" {
		credentials.APIKey = sdkgo.NewSecretString("")
	}
	config := Config{}
	if strings.HasSuffix(scenario.Name, "-smallLimit") {
		config.MaxResponseBytes = 16
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getCurrentWeather": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCurrentWeather(), testConnection),
	}}
}
