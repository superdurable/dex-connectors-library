// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package alapi

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
	reference := sdkgo.ConnectionRef{Provider: "alapi", Name: "alapi"}
	config := Config{}
	if scenario.Name == "lookupIPLocation-oversized" {
		config.MaxResponseBytes = 16
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{reference: {Token: sdkgo.NewSecretString("test-token")}}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"lookupIPLocation": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.LookupIPLocation(), reference),
	}}
}
