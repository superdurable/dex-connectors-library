// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jushuitan

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net/http"
	"testing"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, _ string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{AppKey: sdkgo.NewSecretString("key-123"), AppSecret: sdkgo.NewSecretString("secret-123"), AccessToken: sdkgo.NewSecretString("token-123")}
	config := Config{}
	if scenario.Name == "listShops-oversized" {
		config.MaxResponseBytes = 16
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport), clockOption{now: conformanceFixedClock(t, scenario)})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{"listShops": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListShops(), testConnection)}}
}
