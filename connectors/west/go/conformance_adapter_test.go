// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package west

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
	credentials := Credentials{Username: "zhangsan", APIPassword: sdkgo.NewSecretString("REDACTED_API_PASSWORD")}
	configuration := Config{BaseURL: endpoint}
	if scenario.Name == "listDomains-oversized" {
		configuration.MaxResponseBytes = 8
	}
	client, err := New(configuration, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials},
		WithHTTPClient(transport), clockOption{now: conformanceFixedClock(t, scenario)})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listDomains": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListDomains(), testConnection),
	}}
}
