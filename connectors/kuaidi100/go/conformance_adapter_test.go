// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kuaidi100

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
	credentials := Credentials{Customer: sdkgo.NewSecretString(testCustomer), Key: sdkgo.NewSecretString(testKey)}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"queryTracking": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.QueryTracking(), testConnection),
	}}
}
