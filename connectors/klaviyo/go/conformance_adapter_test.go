// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package klaviyo

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net/http"
	"testing"
)

const conformanceEndpointPath = "/api"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {PrivateAPIKey: sdkgo.NewSecretString("pk_test_key")},
	})
	require.NoError(t, err)
	client.httpClient = transport
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getProfile":  newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.GetProfile(), testConnection),
		"createEvent": newConformanceMutationInvocation(t, scenario.Input, providerContext{}, client.CreateEvent(), testConnection, scenario.CallID),
	}}
}
