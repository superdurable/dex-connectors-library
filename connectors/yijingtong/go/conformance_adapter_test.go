// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package yijingtong

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
	credentials := Credentials{Token: sdkgo.NewSecretString("REDACTED_YIJINGTONG_TOKEN")}
	if scenario.Name == "listWarehouses-defect" {
		credentials = Credentials{}
	}
	configuration := Config{BaseURL: endpoint}
	if scenario.Name == "listWarehouses-oversized" {
		configuration.MaxResponseBytes = 16
	}
	client, err := New(configuration, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listWarehouses": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListWarehouses(), testConnection),
	}}
}
