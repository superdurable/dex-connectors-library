// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shumei

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
	key := "secretTestKey"
	if scenario.Name == "getBalance-defect" {
		key = "bad key"
	}
	credentials := Credentials{AccessKey: sdkgo.NewSecretString(key)}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getBalance": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetBalance(), testConnection),
	}}
}
