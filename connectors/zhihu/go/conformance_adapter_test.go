// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zhihu

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
	credentials := Credentials{AccessSecret: sdkgo.NewSecretString("secretTestToken")}
	if scenario.Name == "listQuotas-defect" {
		credentials = Credentials{AccessSecret: sdkgo.NewSecretString("Bearer bad")}
	}
	configuration := Config{BaseURL: endpoint}
	if scenario.Name == "listQuotas-oversized" {
		configuration.MaxResponseBytes = 16
	}
	client, err := New(configuration, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport), clockOption{now: conformanceFixedClock(t, scenario)})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listQuotas": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListQuotas(), testConnection),
	}}
}
