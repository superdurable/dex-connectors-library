// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wecom

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := sdkgo.StaticCredentialProvider[Credentials]{
		testRobotConnection: {AuthMethodID: groupRobotAuthMethodID, WebhookKey: sdkgo.NewSecretString("REDACTED-WEBHOOK-KEY")},
		testApplicationConnection: {
			AuthMethodID: applicationAuthMethodID,
			CorpID: "ww1a2b3c4d5e6f7a8b", AgentID: "1000002",
			CorpSecret: sdkgo.NewSecretString("REDACTED_CORP_SECRET"),
		},
	}
	client, err := New(Config{BaseURL: endpoint}, credentials, WithHTTPClient(transport))
	require.NoError(t, err)
	context := singledispatchtest.NewContext("wecom-conformance-flow", "wecom-conformance-step")
	client.now = func() time.Time { return context.FirstAttemptAt().Add(time.Second) }
	if len(scenario.PriorHeartbeat) > 0 {
		var previousCheckpoint any
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &previousCheckpoint))
		require.NoError(t, context.SetPersistedCheckpoint(previousCheckpoint))
	}
	currentAttempt := 1
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		for currentAttempt < attempt {
			context.AdvanceAttempt()
			currentAttempt++
		}
	}, actions: map[string]conformanceOperationInvocation{
		"sendGroupRobotMessage": newConformanceMutationInvocation(t, scenario.Input, context, client.SendGroupRobotMessage(), testRobotConnection, scenario.CallID),
		"sendApplicationMessage": newConformanceMutationInvocation(t, scenario.Input, context, client.SendApplicationMessage(), testApplicationConnection, scenario.CallID),
	}}
}
