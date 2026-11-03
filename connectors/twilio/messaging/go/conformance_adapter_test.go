// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package messaging_test

import (
    "encoding/json"
    "net/http"
    "testing"

    "github.com/stretchr/testify/require"
    "github.com/superdurable/dex-connectors-library/connectors/twilio/messaging/go"
    "github.com/superdurable/dex-connectors-library/sdkgo"
    "github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceAccountSID = "AC" + "00000000000000000000000000000000"

const conformanceEndpointPath = "/2010-04-01"
const conformanceFixturePathPrefix = "/2010-04-01"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
    t.Helper()
    client, err := messaging.New(messaging.Config{Endpoint: endpoint, AccountSID: conformanceAccountSID, DefaultSender: testSender},
        sdkgo.StaticCredentialProvider[messaging.Credentials]{twilioConnection: {AuthMethodID: messaging.AuthTokenAuthMethodID, AuthToken: sdkgo.NewSecretString(testAuthToken)}},
        messaging.WithHTTPClient(transport))
    require.NoError(t, err)
    context := singledispatchtest.NewContext("twilio-conformance", "twilio-message")
    if len(scenario.PriorHeartbeat) > 0 {
        var marker any
        require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &marker))
        require.NoError(t, context.SetPersistedCheckpoint(marker))
        context.AdvanceAttempt()
    }
    return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
        if attempt > 1 { context.AdvanceAttempt() }
    }, actions: map[string]conformanceOperationInvocation{
        "sendMessage": newConformanceMutationInvocation(t, scenario.Input, context, client.SendMessage(), twilioConnection, scenario.CallID),
        "getMessage": newConformanceQueryInvocation(t, scenario.Input, context, client.GetMessage(), twilioConnection),
    }}
}
