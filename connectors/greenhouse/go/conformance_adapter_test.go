// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package greenhouse

import (
    "net/http"
    "testing"

    "github.com/stretchr/testify/require"
    "github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v3"
const conformanceFixturePathPrefix = "/v3"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
    t.Helper()
    reference := sdkgo.ConnectionRef{Provider: "greenhouse", Name: "greenhouse"}
    client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
        reference: {ClientID: "sample-client-id", ClientSecret: sdkgo.NewSecretString("sample-client-secret"), AccessToken: sdkgo.NewSecretString("sample-access-token")},
    }, WithHTTPClient(transport))
    require.NoError(t, err)
    return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
        "getCandidate": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCandidate(), reference),
    }}
}
