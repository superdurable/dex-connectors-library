// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack_test

import (
    "net/http"
    "testing"

    "github.com/stretchr/testify/require"
    "github.com/superdurable/dex-connectors-library/connectors/slack/go"
    "github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
    client, err := slack.New(slack.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[slack.Credentials]{
        slackConnection: {BotToken: sdkgo.NewSecretString("bot-token"), UserToken: sdkgo.NewSecretString("user-token")},
    }, slack.WithHTTPClient(transport))
    require.NoError(t, err)
    context := newSlackDexContext("conformance")
    priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { context.heartbeat = value })
    return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
        context.attempt = int32(attempt) + priorAttempt
    }, actions: map[string]conformanceOperationInvocation{
        "listThreadMessages": newConformanceQueryInvocation(t, scenario.Input, context, client.ListThreadMessages(), slackConnection),
        "getThreadReply": newConformanceQueryInvocation(t, scenario.Input, context, client.GetThreadReply(), slackConnection),
        "postChannelMessage": newConformanceMutationInvocation(t, scenario.Input, context, client.PostChannelMessage(), slackConnection, scenario.CallID),
        "postThreadReply": newConformanceMutationInvocation(t, scenario.Input, context, client.PostThreadReply(), slackConnection, scenario.CallID),
    }}
}
