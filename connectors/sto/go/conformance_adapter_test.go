// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sto

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
	credentials := Credentials{AppKey: "app123", AppSecret: sdkgo.NewSecretString("REDACTED_STO_APP_SECRET"), FromCode: "resource123"}
	if scenario.Name == "queryWaybillTrace-defect" {
		credentials = Credentials{}
	}
	configuration := Config{}
	if scenario.Name == "queryWaybillTrace-oversized" {
		configuration.MaxResponseBytes = 16
	}
	client, err := New(configuration, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"queryWaybillTrace": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.QueryWaybillTrace(), testConnection),
	}}
}
