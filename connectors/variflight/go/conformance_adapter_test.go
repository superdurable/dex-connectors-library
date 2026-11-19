// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package variflight

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
	credentials := Credentials{AppID: sdkgo.NewSecretString("test-app"), AppSecurity: sdkgo.NewSecretString("test-secret")}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getFlightStatus": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetFlightStatus(), testConnection),
	}}
}
