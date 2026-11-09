// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package analytics4

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
	reference := sdkgo.ConnectionRef{Provider: "google-analytics-4", Name: "google-analytics-4"}
	credentials := Credentials{
		OAuthClientID: "test-client", OAuthClientSecret: sdkgo.NewSecretString("test-client-secret"),
		AccessToken: sdkgo.NewSecretString("test-analytics-token"), RefreshToken: sdkgo.NewSecretString("test-refresh-token"),
	}
	if scenario.Name == "getProperty-defect" { credentials.AccessToken = sdkgo.NewSecretString("") }
	client, err := New(Config{DataBaseURL: endpoint, AdminBaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"runReport": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.RunReport(), reference),
		"getProperty": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetProperty(), reference),
		"listAudiences": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListAudiences(), reference),
	}}
}
