// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiguang

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	credentials := Credentials{AppKey: "7d431e42dfa6a6d693ac2d04", MasterSecret: sdkgo.NewSecretString("5e987ac6d2e04d95a9d8f0d1")}
	if scenario.Name == "listApplicationTags-invalid-credentials" {
		credentials.AppKey = "bad key"
	}
	config := Config{BaseURL: endpoint}
	if scenario.Name == "listApplicationTags-oversize" {
		config.MaxResponseBytes = 5
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listApplicationTags": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListApplicationTags(), testConnection),
	}}
}
