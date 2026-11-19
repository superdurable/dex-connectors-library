// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pagerduty

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net/http"
	"testing"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("test-secret-token")}}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listIncidents": newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListIncidents(), testConnection),
		"getIncident":   newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetIncident(), testConnection),
	}}
}
