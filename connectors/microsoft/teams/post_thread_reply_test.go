// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teams_test

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/teams"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const testStatusReply = "Status: investigating. Reply ack in this thread to acknowledge."

func validThreadReplyInput() teams.PostThreadReplyInput {
	return teams.PostThreadReplyInput{TeamID: testTeamID, ChannelID: testChannelID, MessageID: testRootID, Content: testStatusReply}
}

func TestPostThreadReplyRepliesToTheRootMessage(t *testing.T) {
	provider := newRecordingGraph(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, chatMessageJSON(t, "1616989753153", testRootID, "", "text", testStatusReply, testCreatedAt))
	})
	client := newTeamsClient(t, provider.endpoint())

	result, err := sdkgo.RunMutation(newTestDexContext("reply"), client.PostThreadReply(), teamsConnection, validThreadReplyInput())
	require.NoError(t, err)
	require.Equal(t, teams.PostThreadReplyBranchSent, result.Branch)
	require.Equal(t, "1616989753153", result.Value.MessageID)
	require.Equal(t, testRootID, result.Value.ReplyToID)
	require.Equal(t, testUserID, result.Value.Sender.UserID)
	request := provider.request(0)
	require.Equal(t, testRepliesPath, request.path)
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(request.body), &body))
	require.Equal(t, map[string]any{"body": map[string]any{"contentType": "text", "content": testStatusReply}}, body,
		"a reply carries no subject, and blank importance is left to Teams")
}

func TestPostThreadReplyRejectsAnInvalidRootMessageID(t *testing.T) {
	client := newTeamsClient(t, "http://127.0.0.1:1/v1.0")
	input := validThreadReplyInput()
	input.MessageID = "../../messages"
	result, err := sdkgo.RunMutation(newTestDexContext("invalid"), client.PostThreadReply(), teamsConnection, input)
	require.NoError(t, err)
	require.Equal(t, teams.PostThreadReplyBranchDefect, result.Branch)
	require.Equal(t, teams.PostMessageOutput{TeamID: testTeamID, ChannelID: testChannelID, ReplyToID: "../../messages"}, result.Value)
}

func TestUnconfirmedReplyIsConfirmedOnlyByAReplyInTheSameThread(t *testing.T) {
	var mutex sync.Mutex
	replies := collectionJSON("")
	provider := newRecordingGraph(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			mutex.Lock()
			replies = collectionJSON("",
				chatMessageJSON(t, "1616989753160", "1616000000000", "", "text", testStatusReply, time.Now()),
				chatMessageJSON(t, "1616989753153", testRootID, "", "html", "<p>Status: investigating. Reply ack in this thread to acknowledge.</p>", time.Now()),
			)
			mutex.Unlock()
			<-request.Context().Done()
			return
		}
		require.Equal(t, testRepliesPath, request.URL.Path)
		mutex.Lock()
		body := replies
		mutex.Unlock()
		writeJSON(t, response, http.StatusOK, body)
	})
	client := newTeamsClient(t, provider.endpoint(), teams.WithHTTPClient(&http.Client{Timeout: 300 * time.Millisecond}))
	dexContext := newTestDexContext("reply-timeout")

	_, err := sdkgo.RunMutation(dexContext, client.PostThreadReply(), teamsConnection, validThreadReplyInput())
	requireRetry(t, err, sdkgo.FailureTransport)
	result, err := sdkgo.RunMutation(dexContext, client.PostThreadReply(), teamsConnection, validThreadReplyInput())
	require.NoError(t, err)
	require.Equal(t, teams.PostThreadReplyBranchSent, result.Branch)
	require.True(t, result.Value.IsConfirmedByReadBack)
	require.Equal(t, "1616989753153", result.Value.MessageID, "a matching reply in another thread is ignored")
	require.Equal(t, 1, provider.countRequests(http.MethodPost))
}
