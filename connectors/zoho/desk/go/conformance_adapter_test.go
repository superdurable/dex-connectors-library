// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk_test

import (
    "encoding/json"
    "net/http"
    "testing"

    "github.com/stretchr/testify/require"
    "github.com/superdurable/dex-connectors-library/connectors/zoho/desk/go"
)

const conformanceEndpointPath = "/api/v1"
const conformanceFixturePathPrefix = "/api/v1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
    t.Helper()
    client, err := desk.New(desk.Config{OrgID: testOrganizationID}, testCredentialProvider(), desk.WithAPIBaseURL(endpoint), desk.WithHTTPClient(transport))
    require.NoError(t, err)
    context := newDeskDexContext("conformance")
    previousAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { context.previousHeartbeat = append(json.RawMessage(nil), value...) })
    return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
        context.attempt = previousAttempt + int32(attempt)
        if attempt > 1 {
            context.previousHeartbeat = append(json.RawMessage(nil), context.recordedHeartbeat...)
            context.recordedHeartbeat = nil
        }
    }, actions: map[string]conformanceOperationInvocation{
        "searchTickets": newConformanceQueryInvocation(t, scenario.Input, context, client.SearchTickets(), deskConnection),
        "getTicket": newConformanceQueryInvocation(t, scenario.Input, context, client.GetTicket(), deskConnection),
        "createTicket": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateTicket(), deskConnection, scenario.CallID),
        "updateTicket": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateTicket(), deskConnection, scenario.CallID),
        "addComment": newConformanceMutationInvocation(t, scenario.Input, context, client.AddComment(), deskConnection, scenario.CallID),
    }}
}
