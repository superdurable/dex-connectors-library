// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/slack/go"
	"github.com/superdurable/dex-connectors-library/connectors/slack/go/slackmock"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// Binding the hand-written client fails if its Definitions drift from the generated agent metadata.
func TestClientAgentToolsClassifyProviderOutcomes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		channelID := request.URL.Query().Get("channel")
		if request.URL.Path == "/chat.postMessage" {
			var payload map[string]string
			require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
			channelID = payload["channel"]
		}
		var body string
		switch {
		case channelID == "C404":
			body = `{"ok":false,"error":"channel_not_found"}`
		case request.URL.Path == "/conversations.replies" && request.URL.Query().Get("oldest") == "9.0":
			body = `{"ok":true,"messages":[{"ts":"3.0","thread_ts":"1.0","user":"U2","text":"other"}]}`
		case request.URL.Path == "/conversations.replies":
			body = `{"ok":true,"messages":[{"ts":"2.0","thread_ts":"1.0","user":"U2","text":"approve"}]}`
		case request.URL.Path == "/chat.postMessage":
			body = `{"ok":true,"channel":"C123","ts":"4.0","message":{"ts":"4.0","user":"UBOT","text":"posted"}}`
		default:
			t.Errorf("unexpected Slack request %s", request.URL.Path)
		}
		_, err := response.Write([]byte(body))
		require.NoError(t, err)
	}))
	defer server.Close()
	connection, err := slack.NewConnection(newSlackClient(t, server.URL), slackConnection)
	require.NoError(t, err)

	tools, err := connection.AgentTools()
	require.NoError(t, err)
	require.Equal(t, []string{"slack_list_thread_messages", "slack_get_thread_reply", "slack_post_channel_message", "slack_post_thread_reply"}, agentToolNames(tools))
	require.Equal(t, agentToolNames(tools), agentToolMetadataNames(slack.ExposedAgentTools()))
	effects := make([]sdkgo.AgentToolEffect, 0, len(tools))
	for _, tool := range tools {
		effects = append(effects, tool.Metadata().Effect())
		require.Equal(t, connection.Reference(), tool.Connection())
	}
	require.Equal(t, []sdkgo.AgentToolEffect{sdkgo.AgentToolEffectRead, sdkgo.AgentToolEffectRead, sdkgo.AgentToolEffectNonIdempotentWrite, sdkgo.AgentToolEffectNonIdempotentWrite}, effects)
	listThreadMessages, getThreadReply, postChannelMessage, postThreadReply := tools[0], tools[1], tools[2], tools[3]

	attempts := []struct {
		name      string
		tool      sdkgo.AgentTool
		arguments string
		outcome   sdkgo.AgentToolAttemptOutcome
		branch    sdkgo.BranchID
	}{
		{"listThreadMessages read", listThreadMessages, `{"channelId":"C123","threadTimestamp":"1.0","pageSize":15}`, sdkgo.AgentToolAttemptSucceeded, slack.ListThreadMessagesBranchRead},
		{"listThreadMessages providerRejected", listThreadMessages, `{"channelId":"C404","threadTimestamp":"1.0","pageSize":15}`, sdkgo.AgentToolAttemptRejected, slack.ListThreadMessagesBranchProviderRejected},
		{"getThreadReply found", getThreadReply, `{"channelId":"C123","threadTimestamp":"1.0","replyTimestamp":"2.0"}`, sdkgo.AgentToolAttemptSucceeded, slack.GetThreadReplyBranchFound},
		{"getThreadReply notFound", getThreadReply, `{"channelId":"C123","threadTimestamp":"1.0","replyTimestamp":"9.0"}`, sdkgo.AgentToolAttemptRejected, slack.GetThreadReplyBranchNotFound},
		{"postChannelMessage sent", postChannelMessage, `{"channelId":"C123","text":"posted"}`, sdkgo.AgentToolAttemptSucceeded, slack.PostChannelMessageBranchSent},
		// The singledispatch Fence claimed a dispatch, so even a definite Slack refusal is unknown.
		{"postChannelMessage providerRejected", postChannelMessage, `{"channelId":"C404","text":"posted"}`, sdkgo.AgentToolAttemptUnknown, slack.PostChannelMessageBranchProviderRejected},
		{"postThreadReply sent", postThreadReply, `{"channelId":"C123","threadTimestamp":"1.0","text":"posted"}`, sdkgo.AgentToolAttemptSucceeded, slack.PostThreadReplyBranchSent},
		// A Mutation defect is unknown even though it is selected before chat.postMessage is sent.
		{"postThreadReply defect", postThreadReply, `{"channelId":"C123","threadTimestamp":"1.0","text":""}`, sdkgo.AgentToolAttemptUnknown, slack.PostThreadReplyBranchDefect},
	}
	for _, expected := range attempts {
		t.Run(expected.name, func(t *testing.T) {
			attempt, err := expected.tool.RunAttempt(newSlackDexContext(expected.name), json.RawMessage(expected.arguments))
			require.NoError(t, err)
			require.Equal(t, expected.outcome, attempt.Outcome)
			require.Equal(t, expected.branch, attempt.Branch)
			require.True(t, attempt.HasInvokedOperation)
		})
	}
}

func TestMockConnectionAgentToolsRunScriptedBranches(t *testing.T) {
	mock := slackmock.New(t, "mock")
	mock.GetThreadReply().Respond(slackmock.GetThreadReplyNotFound(slack.GetThreadReplyOutput{}, &sdkgo.Failure{Kind: sdkgo.FailureNotFound, Provider: "slack", Operation: "getThreadReply", Message: "Slack reply was not found"}))
	mock.PostChannelMessage().Respond(slackmock.PostChannelMessageProviderRejected(slack.PostMessageOutput{}, &sdkgo.Failure{Kind: sdkgo.FailureNotFound, Provider: "slack", Operation: "postChannelMessage", Message: "Slack channel was not found"}))

	tools, err := mock.Connection().AgentTools()
	require.NoError(t, err)
	require.Equal(t, agentToolMetadataNames(slack.ExposedAgentTools()), agentToolNames(tools))

	attempt, err := tools[0].RunAttempt(newSlackDexContext("mock-list"), json.RawMessage(`{"channelId":"C123","threadTimestamp":"1.0","pageSize":15}`))
	require.NoError(t, err)
	require.Equal(t, sdkgo.AgentToolAttemptSucceeded, attempt.Outcome)

	attempt, err = tools[1].RunAttempt(newSlackDexContext("mock-get"), json.RawMessage(`{"channelId":"C123","threadTimestamp":"1.0","replyTimestamp":"2.0"}`))
	require.NoError(t, err)
	require.Equal(t, sdkgo.AgentToolAttemptRejected, attempt.Outcome)
	require.Equal(t, slack.GetThreadReplyBranchNotFound, attempt.Branch)

	// The mock uses no singledispatch Fence, so the listed rejected outcome applies.
	attempt, err = tools[2].RunAttempt(newSlackDexContext("mock-post"), json.RawMessage(`{"channelId":"C404","text":"posted"}`))
	require.NoError(t, err)
	require.Equal(t, sdkgo.AgentToolAttemptRejected, attempt.Outcome)
	require.Equal(t, slack.PostChannelMessageBranchProviderRejected, attempt.Branch)

	attempt, err = tools[3].RunAttempt(newSlackDexContext("mock-reply"), json.RawMessage(`{"channelId":"C123","threadTimestamp":"1.0","text":"posted"}`))
	require.NoError(t, err)
	require.Equal(t, sdkgo.AgentToolAttemptSucceeded, attempt.Outcome)
}

func agentToolNames(tools []sdkgo.AgentTool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Metadata().ToolName)
	}
	return names
}

func agentToolMetadataNames(metadata []sdkgo.AgentToolMetadata) []string {
	names := make([]string, 0, len(metadata))
	for _, tool := range metadata {
		names = append(names, tool.ToolName)
	}
	return names
}
