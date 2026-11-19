// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kf5

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
	credentials := Credentials{Subdomain: "sample", Email: sdkgo.NewSecretString("agent@example.com"), APIToken: sdkgo.NewSecretString("private-token")}

	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getTicket": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetTicket(), testConnection),
	}}
}
