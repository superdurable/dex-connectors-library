// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package telegram

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {BotToken: sdkgo.NewSecretString(testBotToken)},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := singledispatchtest.NewContext("telegram-conformance", scenario.Name)
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"getChatAdministrators": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetChatAdministrators(), testConnection),
		"getChatMember":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetChatMember(), testConnection),
		"leaveChat":             newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.LeaveChat(), testConnection, scenario.CallID),
		"setChatDescription":    newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.SetChatDescription(), testConnection, scenario.CallID),
		"setChatTitle":          newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.SetChatTitle(), testConnection, scenario.CallID),
		"sendMessage":           newConformanceMutationInvocation(t, scenario.Input, context, client.SendMessage(), testConnection, scenario.CallID),
		"getChat":               newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetChat(), testConnection),
	}}
}
