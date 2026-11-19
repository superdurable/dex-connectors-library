// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onebound

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
	credentials := Credentials{APIKey: sdkgo.NewSecretString("testKey"), APISecret: sdkgo.NewSecretString("testSecret")}
	config := Config{BaseURL: endpoint}
	if scenario.Name == "searchProducts-oversized" {
		config.MaxResponseBytes = 16
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport), clockOption{now: conformanceFixedClock(t, scenario)})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{"searchProducts": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.SearchProducts(), testConnection)}}
}
