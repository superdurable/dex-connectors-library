// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onenet

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
	credentials := Credentials{UserID: "130037", AccessKey: sdkgo.NewSecretString("dGVzdC1hY2Nlc3Mta2V5"), ProductID: "demo123"}
	config := Config{BaseURL: endpoint}
	if scenario.Name == "queryDeviceDetail-oversized" {
		config.MaxResponseBytes = 5
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport), clockOption{now: conformanceFixedClock(t, scenario)})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{"queryDeviceDetail": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.QueryDeviceDetail(), testConnection)}}
}
