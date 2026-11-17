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
		"sendAnimation":         newConformanceMutationInvocation(t, scenario.Input, context, client.SendAnimation(), testConnection, scenario.CallID),
		"sendAudio":             newConformanceMutationInvocation(t, scenario.Input, context, client.SendAudio(), testConnection, scenario.CallID),
		"sendDocument":          newConformanceMutationInvocation(t, scenario.Input, context, client.SendDocument(), testConnection, scenario.CallID),
		"sendPhoto":             newConformanceMutationInvocation(t, scenario.Input, context, client.SendPhoto(), testConnection, scenario.CallID),
		"sendSticker":           newConformanceMutationInvocation(t, scenario.Input, context, client.SendSticker(), testConnection, scenario.CallID),
		"sendVideo":             newConformanceMutationInvocation(t, scenario.Input, context, client.SendVideo(), testConnection, scenario.CallID),
		"sendLocation":          newConformanceMutationInvocation(t, scenario.Input, context, client.SendLocation(), testConnection, scenario.CallID),
		"sendMediaGroup":        newConformanceMutationInvocation(t, scenario.Input, context, client.SendMediaGroup(), testConnection, scenario.CallID),
		"editMessageText":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.EditMessageText(), testConnection, scenario.CallID),
		"deleteMessage":         newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteMessage(), testConnection, scenario.CallID),
		"pinChatMessage":        newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.PinChatMessage(), testConnection, scenario.CallID),
		"unpinChatMessage":      newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UnpinChatMessage(), testConnection, scenario.CallID),
		"sendChatAction":        newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.SendChatAction(), testConnection, scenario.CallID),
		"answerCallbackQuery":   newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.AnswerCallbackQuery(), testConnection, scenario.CallID),
		"answerInlineQuery":     newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.AnswerInlineQuery(), testConnection, scenario.CallID),
		"getFile":               newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetFile(), testConnection),
		"getChatAdministrators": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetChatAdministrators(), testConnection),
		"getChatMember":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetChatMember(), testConnection),
		"leaveChat":             newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.LeaveChat(), testConnection, scenario.CallID),
		"setChatDescription":    newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.SetChatDescription(), testConnection, scenario.CallID),
		"setChatTitle":          newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.SetChatTitle(), testConnection, scenario.CallID),
		"sendMessage":           newConformanceMutationInvocation(t, scenario.Input, context, client.SendMessage(), testConnection, scenario.CallID),
		"getChat":               newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetChat(), testConnection),
	}}
}
