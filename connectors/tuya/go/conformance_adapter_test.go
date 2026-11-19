// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tuya

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
	config := Config{}
	if scenario.Name == "getDeviceDetails-oversize" {
		config.MaxResponseBytes = 8
	}
	credentials := Credentials{ClientID: sdkgo.NewSecretString("sample-client-id"), ClientSecret: sdkgo.NewSecretString("sample-client-secret")}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	client.endpoint = endpoint
	client.now = conformanceFixedClock(t, scenario)
	client.nonceReader = newConformanceSyntheticRandomReader(scenario.RandomSeed)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getDeviceDetails": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetDeviceDetails(), testConnection),
	}}
}
