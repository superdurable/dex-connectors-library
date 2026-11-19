// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hby365

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
	reference := sdkgo.ConnectionRef{Provider: "hby365", Name: "hby365"}
	config := Config{}
	if scenario.Name == "listModels-oversized" {
		config.MaxResponseBytes = 16
	}
	credentials := Credentials{APIKey: sdkgo.NewSecretString("test-key")}
	if scenario.Name == "listModels-defect" {
		credentials.APIKey = sdkgo.NewSecretString("")
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listModels": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListModels(), reference),
	}}
}
