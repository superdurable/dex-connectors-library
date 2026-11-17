// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package chukou1

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
	if scenario.Name == "listWarehouses-oversize" {
		config.MaxResponseBytes = 4
	}
	credentials := Credentials{AccessToken: sdkgo.NewSecretString("secretTestToken")}
	if scenario.Name == "listWarehouses-missing-credentials" {
		credentials = Credentials{}
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listWarehouses": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListWarehouses(), testConnection),
	}}
}
