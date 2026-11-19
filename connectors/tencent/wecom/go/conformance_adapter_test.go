// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wecom

import (
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
	robotConnection := sdkgo.ConnectionRef{Provider: "wecom", Name: "wecom-robot"}
	applicationConnection := sdkgo.ConnectionRef{Provider: "wecom", Name: "wecom-app"}
	credentials := sdkgo.StaticCredentialProvider[Credentials]{
		robotConnection: {AuthMethodID: groupRobotAuthMethodID, WebhookKey: sdkgo.NewSecretString("REDACTED-WECOM-WEBHOOK-KEY")},
		applicationConnection: {
			AuthMethodID: applicationAuthMethodID, CorpID: testCorpID, AgentID: testAgentID,
			CorpSecret: sdkgo.NewSecretString("REDACTED_WECOM_CORP_SECRET"),
		},
	}
	client, err := New(Config{BaseURL: endpoint}, credentials, WithHTTPClient(transport))
	require.NoError(t, err)
	context := singledispatchtest.NewContext(scenario.Name, "send-step")
	now := context.FirstAttemptAt().Add(time.Minute)
	client.now = func() time.Time { return now }
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		for context.Attempt() < int32(attempt) {
			context.AdvanceAttempt()
		}
		if scenario.Name == "sendApplicationMessage-uncertain-cutoff" && attempt > 1 {
			now = context.FirstAttemptAt().Add(DuplicateCheckResendCutoff)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"sendGroupRobotMessage": newConformanceMutationInvocation(t, scenario.Input, context, client.SendGroupRobotMessage(), robotConnection, scenario.CallID),
		"sendApplicationMessage": newConformanceMutationInvocation(t, scenario.Input, context, client.SendApplicationMessage(), applicationConnection, scenario.CallID),
	}}
}
