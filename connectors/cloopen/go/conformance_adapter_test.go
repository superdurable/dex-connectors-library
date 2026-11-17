// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cloopen

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	config := Config{BaseURL: endpoint}
	if scenario.Name == "getBalance-oversize" {
		config.MaxResponseBytes = 4
	}
	credentials := Credentials{AccountSID: sdkgo.NewSecretString(testAccountSID), AuthToken: sdkgo.NewSecretString(testAuthToken)}
	if scenario.Name == "getBalance-missing-credentials" {
		credentials = Credentials{}
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials},
		WithHTTPClient(transport), WithClock(conformanceFixedClock(t, scenario)))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getBalance": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetBalance(), testConnection),
	}}
}
