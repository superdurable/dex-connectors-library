// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday_test

import (
    "encoding/json"
    "net/http"
    "testing"

    "github.com/stretchr/testify/require"
    "github.com/superdurable/dex-connectors-library/connectors/monday/go"
)

const conformanceEndpointPath = "/v2"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
    t.Helper()
    client, err := monday.New(monday.Config{}, testCredentialProvider(), monday.WithAPIURL(endpoint), monday.WithHTTPClient(transport), monday.WithClock(conformanceFixedClock(t, scenario)))
    require.NoError(t, err)
    context := newMondayDexContext("conformance")
    var initialAttempt int32
    if len(scenario.PriorHeartbeat) > 0 {
        var prior struct {
            Attempt int32 `json:"attempt"`
            Value json.RawMessage `json:"value"`
        }
        require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &prior))
        initialAttempt = prior.Attempt
        context.heartbeat = append(json.RawMessage(nil), prior.Value...)
    }
    return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
        context.attemptNo = initialAttempt + int32(attempt)
    }, actions: map[string]conformanceOperationInvocation{
        "listItems": newConformanceQueryInvocation(t, scenario.Input, context, client.ListItems(), mondayConnection),
        "getItem": newConformanceQueryInvocation(t, scenario.Input, context, client.GetItem(), mondayConnection),
        "createItem": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateItem(), mondayConnection, scenario.CallID),
        "updateItemColumnValues": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateItemColumnValues(), mondayConnection, scenario.CallID),
        "addUpdate": newConformanceMutationInvocation(t, scenario.Input, context, client.AddUpdate(), mondayConnection, scenario.CallID),
    }}
}
