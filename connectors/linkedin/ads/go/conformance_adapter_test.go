// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ads

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "linkedin-ads", Name: "linkedin-ads"}
	credentials := Credentials{
		OAuthClientID: "synthetic-client-id", OAuthClientSecret: sdkgo.NewSecretString("synthetic-client-secret"),
		AccessToken: sdkgo.NewSecretString("test-linkedin-ads-token"), RefreshToken: sdkgo.NewSecretString("synthetic-refresh-token"),
	}
	config := Config{BaseURL: endpoint}
	if strings.HasPrefix(scenario.Name, "listAdAccounts-oversized") {
		config.MaxResponseBytes = 16
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listAdAccounts": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListAdAccounts(), reference),
	}}
}
