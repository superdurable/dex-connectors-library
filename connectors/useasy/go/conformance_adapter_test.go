// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package useasy

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
	credentials := Credentials{AppID: sdkgo.NewSecretString(testAppID), SecretKey: sdkgo.NewSecretString(testSecretKey)}
	if scenario.Name == "listAgents-defect" {
		credentials = Credentials{}
	}
	configuration := Config{BaseURL: "https://openapi.useasy.cn"}
	if scenario.Name == "listAgents-oversized" {
		configuration.MaxResponseBytes = 16
	}
	client, err := New(configuration, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	client.nonce = func() (string, error) { return "123456", nil }
	client.now = func() time.Time { return time.Unix(1731510000, 0) }
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listAgents": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListAgents(), testConnection),
	}}
}
