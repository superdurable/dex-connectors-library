// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qiyuesuo

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{AppToken: sdkgo.NewSecretString(testAppToken), AppSecret: sdkgo.NewSecretString(testAppSecret)}
	if scenario.Name == "listContracts-defect" {
		credentials = Credentials{}
	}
	configuration := Config{BaseURL: "https://openapi.qiyuesuo.com"}
	if scenario.Name == "listContracts-oversized" {
		configuration.MaxResponseBytes = 16
	}
	client, err := New(configuration, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	client.requestNonce = func() (string, error) { return "254775d7-3c7c-4e9d-9880-1d4c5fb3dbd0", nil }
	client.now = func() time.Time { return time.UnixMilli(1731510000000) }
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listContracts": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContracts(), testConnection),
	}}
}
