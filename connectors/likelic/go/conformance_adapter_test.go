// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package likelic

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
	credentials := Credentials{CustomerCode: "demo_company", APIKey: sdkgo.NewSecretString(testAPIKey)}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listRooms": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListRooms(), testConnection),
	}}
}
