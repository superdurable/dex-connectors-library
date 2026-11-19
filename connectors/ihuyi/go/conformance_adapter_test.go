// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ihuyi

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
	credentials := Credentials{APIID: "test-account", APIKey: sdkgo.NewSecretString("private-test-key")}
	config := Config{}
	if scenario.Name == "lookupIPLocation-oversized" {
		config.MaxResponseBytes = 16
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport), clockOption{now: conformanceFixedClock(t, scenario)})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{"lookupIPLocation": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.LookupIPLocation(), testConnection)}}
}
