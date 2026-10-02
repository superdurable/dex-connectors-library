// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teams_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/teams"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const testEscalation = "INC-1042 has had no acknowledgement for 10 minutes."

func validChatMessageInput() teams.PostChatMessageInput {
	return teams.PostChatMessageInput{ChatID: testChatID, Content: testEscalation, Importance: teams.MessageImportanceUrgent}
}

func TestPostChatMessageSendsToTheChat(t *testing.T) {
	provider := newRecordingGraph(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, chatMessageJSON(t, "1616991463150", "", "", "text", testEscalation, testCreatedAt))
	})
	client := newTeamsClient(t, provider.endpoint())

	result, err := sdkgo.RunMutation(newTestDexContext("chat"), client.PostChatMessage(), teamsConnection, validChatMessageInput())
	require.NoError(t, err)
	require.Equal(t, teams.PostChatMessageBranchSent, result.Branch)
	require.Equal(t, "1616991463150", result.Value.MessageID)
	require.Equal(t, testChatID, result.Value.ChatID)
	require.Equal(t, testChatMessagePath, provider.request(0).path)
	require.JSONEq(t, `{"importance":"urgent","body":{"contentType":"text","content":"`+testEscalation+`"}}`, provider.request(0).body)
}

func TestUnconfirmedChatMessageIsUncertainWithoutAReadOrResend(t *testing.T) {
	provider := newRecordingGraph(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		<-request.Context().Done()
	})
	client := newTeamsClient(t, provider.endpoint(), teams.WithHTTPClient(&http.Client{Timeout: 200 * time.Millisecond}))
	dexContext := newTestDexContext("chat-timeout")

	result, err := sdkgo.RunMutation(dexContext, client.PostChatMessage(), teamsConnection, validChatMessageInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Equal(t, teams.PostMessageOutput{ChatID: testChatID}, result.Value)

	result, err = sdkgo.RunMutation(dexContext, client.PostChatMessage(), teamsConnection, validChatMessageInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch, "a later attempt of the same Step execution never sends")
	require.Contains(t, result.Failure.Message, "cannot read it back")
	require.Equal(t, 1, provider.requestCount())
}

func TestPostChatMessageRejectsAChannelIDAsAChat(t *testing.T) {
	client := newTeamsClient(t, "http://127.0.0.1:1/v1.0")
	result, err := sdkgo.RunMutation(newTestDexContext("chat-invalid"), client.PostChatMessage(), teamsConnection,
		teams.PostChatMessageInput{ChatID: "19:abc@thread.tacv2/../../me", Content: "hello"})
	require.NoError(t, err)
	require.Equal(t, teams.PostChatMessageBranchDefect, result.Branch)
}
