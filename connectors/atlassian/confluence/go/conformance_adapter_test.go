// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence_test

import (
    "encoding/json"
    "net/http"
    "testing"
    "time"

    "github.com/stretchr/testify/require"
    "github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence/go"
    "github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
    t.Helper()
    client, err := confluence.New(confluence.Config{Endpoint: endpoint, CloudID: testCloudID}, sdkgo.StaticCredentialProvider[confluence.Credentials]{
        confluenceConnection: {
            OAuthClientID: "confluence-test-client",
            OAuthClientSecret: sdkgo.NewSecretString("confluence-test-client-secret"),
            AccessToken: sdkgo.NewSecretString(testAccessToken),
            RefreshToken: sdkgo.NewSecretString("confluence-test-refresh-token"),
        },
    }, confluence.WithHTTPClient(transport))
    require.NoError(t, err)
    context := newTestDexContext(scenario.Name)
    context.firstAttemptAt = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
    priorAttempt := int32(0)
    if len(scenario.PriorHeartbeat) > 0 {
        var prior struct {
            Attempt int32 `json:"attempt"`
        }
        require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &prior))
        priorAttempt = prior.Attempt
        context.heartbeat = append([]byte(nil), scenario.PriorHeartbeat...)
    }
    // Each scenario attempt follows the prior heartbeat's attempt, as Dex numbers them.
    before := func(attempt int) { context.attempt = priorAttempt + int32(attempt) }
    return &conformanceMappedInvoker{t: t, scenario: scenario, before: before, actions: map[string]conformanceOperationInvocation{
        "searchPages": newConformanceQueryInvocation(t, scenario.Input, context, client.SearchPages(), confluenceConnection),
        "getPage": newConformanceQueryInvocation(t, scenario.Input, context, client.GetPage(), confluenceConnection),
        "createPage": newConformanceMutationInvocation(t, scenario.Input, context, client.CreatePage(), confluenceConnection, scenario.CallID),
        "updatePage": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdatePage(), confluenceConnection, scenario.CallID),
        "addComment": newConformanceMutationInvocation(t, scenario.Input, context, client.AddComment(), confluenceConnection, scenario.CallID),
    }}
}
