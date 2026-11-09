// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tagmanager

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
    reference := sdkgo.ConnectionRef{Provider: "google-tag-manager", Name: "google-tag-manager"}
    credentials := Credentials{
        OAuthClientID: "test-client", OAuthClientSecret: sdkgo.NewSecretString("test-client-secret"),
        AccessToken: sdkgo.NewSecretString("test-tag-manager-token"), RefreshToken: sdkgo.NewSecretString("test-refresh-token"),
    }
    if scenario.Name == "listContainers-defect" { credentials.AccessToken = sdkgo.NewSecretString("") }
    client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials}, WithHTTPClient(transport))
    require.NoError(t, err)
    return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
        "listAccounts": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListAccounts(), reference),
        "listContainers": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContainers(), reference),
        "listTags": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTags(), reference),
    }}
}
