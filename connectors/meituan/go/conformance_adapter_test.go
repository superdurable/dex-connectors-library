// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meituan

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
	credentials := Credentials{AppKey: "test-app-key", Secret: sdkgo.NewSecretString("private-secret")}
	config := Config{BaseURL: endpoint}
	if scenario.Name == "queryOrderStatus-oversized" {
		config.MaxResponseBytes = 16
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport), clockOption{now: conformanceFixedClock(t, scenario)})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{"queryOrderStatus": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.QueryOrderStatus(), testConnection)}}
}
