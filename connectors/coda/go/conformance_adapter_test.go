// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package coda

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/apis/v1"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	connection := sdkgo.ConnectionRef{Provider: "coda", Name: "coda"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		connection: {APIToken: sdkgo.NewSecretString("test-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listRows": newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.ListRows(), connection),
		"getRow":   newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.GetRow(), connection),
	}}
}
