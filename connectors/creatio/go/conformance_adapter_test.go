// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package creatio

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
	reference := sdkgo.ConnectionRef{Provider: "creatio", Name: "creatio"}
	client, err := New(Config{BaseURL: endpoint, TokenEndpoint: endpoint + "/connect/token"}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {
			ClientID: "sample-client-id", ClientSecret: sdkgo.NewSecretString("sample-client-secret"),
			AccessToken: sdkgo.NewSecretString("sample-access-token"),
		},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getContact": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetContact(), reference),
	}}
}
