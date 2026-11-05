// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bitbucket

import (
    "net/http"
    "testing"

    "github.com/stretchr/testify/require"
    "github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/2.0"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
    client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
        testConnection: {AccessToken: sdkgo.NewSecretString("test-token")},
    }, WithHTTPClient(transport))
    require.NoError(t, err)
    return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
        "listRepositories": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListRepositories(), testConnection),
        "listPullRequests": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListPullRequests(), testConnection),
    }}
}
