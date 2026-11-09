// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workday

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
	reference := sdkgo.ConnectionRef{Provider: "workday", Name: "workday"}
	client, err := New(Config{BaseURL: endpoint + "/api/common/v1/demo", AuthorizationEndpoint: endpoint + "/authorize", TokenEndpoint: endpoint + "/token", Scopes: []string{"Staffing"}}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {
			OAuthClientID: "synthetic-client-id", OAuthClientSecret: sdkgo.NewSecretString("synthetic-client-secret"),
			AccessToken: sdkgo.NewSecretString("test-workday-token"), RefreshToken: sdkgo.NewSecretString("synthetic-refresh-token"),
		},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getWorker": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetWorker(), reference),
	}}
}
