// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package suxin

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
	credentials := Credentials{AppID: sdkgo.NewSecretString("app_test_123"), APIKey: sdkgo.NewSecretString("sk_test_123")}
	if scenario.Name == "listOrganizations-defect" {
		credentials.AppID = sdkgo.NewSecretString("")
	}
	client, err := New(Config{BaseURL: "https://pc.suxin365.com"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listOrganizations": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListOrganizations(), testConnection),
	}}
}
