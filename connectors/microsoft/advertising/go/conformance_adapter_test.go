// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package microsoftadvertising

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
	reference := sdkgo.ConnectionRef{Provider: "microsoft-advertising", Name: "microsoft-advertising"}
	client, err := New(Config{BaseURL: endpoint, CustomerID: "73456"}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {
			OAuthClientID: "synthetic-client-id", OAuthClientSecret: sdkgo.NewSecretString("synthetic-client-secret"),
			AccessToken: sdkgo.NewSecretString("test-microsoft-advertising-token"), RefreshToken: sdkgo.NewSecretString("synthetic-refresh-token"), DeveloperToken: sdkgo.NewSecretString("test-developer-token"),
		},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listCampaigns": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListCampaigns(), reference),
	}}
}
