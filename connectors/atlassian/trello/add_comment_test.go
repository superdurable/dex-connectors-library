// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const commentActionJSON = `{"id":"` + testActionID + `","idMemberCreator":"` + testMemberID + `","type":"commentCard",` +
	`"date":"2026-09-30T16:20:00.396Z","data":{"text":"SENTINEL echo","card":{"id":"` + testCardID + `"}}}`

func TestAddCommentSendsThePlainTextOnceInTheBody(t *testing.T) {
	provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, commentActionJSON)
	})
	ctx := newTrelloDexContext("comment")
	result, err := sdkgo.RunMutation(ctx, newTrelloClient(t, provider.URL).AddComment(), trelloConnection,
		trello.AddCommentInput{CardID: testCardID, Text: "Approved by Grace Hopper.\n\n**Budget** code FAC-7."})
	require.NoError(t, err)
	require.Equal(t, trello.AddCommentBranchAdded, result.Branch)
	createdAt := time.Date(2026, 9, 30, 16, 20, 0, 396000000, time.UTC)
	require.Equal(t, trello.AddCommentOutput{CardID: testCardID, CommentID: testActionID, AuthorMemberID: testMemberID, CreatedAt: &createdAt}, result.Value)
	require.Equal(t, testActionID, result.Receipt.ProviderObjectID)
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/cards/"+testCardID+"/actions/comments", request.path)
	require.Empty(t, request.rawQuery, "comment text travels in the JSON body, never in the URL")
	require.JSONEq(t, `{"text":"Approved by Grace Hopper.\n\n**Budget** code FAC-7."}`, request.body)
	require.JSONEq(t, `{"trelloDispatchedCallId":"`+string(result.Receipt.CallID)+`"}`, string(ctx.recordedHeartbeat))
	requireNoSentinel(t, result)
}

func TestAddCommentBranches(t *testing.T) {
	for name, test := range map[string]struct {
		reply  func(http.ResponseWriter)
		branch sdkgo.BranchID
	}{
		"missing card":   {func(response http.ResponseWriter) { writeText(t, response, http.StatusNotFound, "SENTINEL") }, trello.AddCommentBranchNotFound},
		"no permission":  {func(response http.ResponseWriter) { writeText(t, response, http.StatusUnauthorized, "SENTINEL") }, trello.AddCommentBranchProviderRejected},
		"server error":   {func(response http.ResponseWriter) { writeText(t, response, http.StatusServiceUnavailable, "SENTINEL") }, trello.AddCommentBranchUncertain},
		"lost response":  {func(response http.ResponseWriter) { dropConnection(t, response) }, trello.AddCommentBranchUncertain},
		"unusable reply": {func(response http.ResponseWriter) { writeJSON(t, response, http.StatusOK, `{"type":"commentCard"}`) }, trello.AddCommentBranchUncertain},
	} {
		provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) { test.reply(response) })
		ctx := newTrelloDexContext("comment-branches")
		result, err := sdkgo.RunMutation(ctx, newTrelloClient(t, provider.URL).AddComment(), trelloConnection,
			trello.AddCommentInput{CardID: testCardID, Text: "Approved."})
		require.NoError(t, err, name)
		require.Equal(t, test.branch, result.Branch, name)
		require.Equal(t, testCardID, result.Value.CardID, name)
		require.Empty(t, result.Value.CommentID, name)
		require.Equal(t, 1, provider.requestCount(), "%s: a comment is never resent", name)
		require.NotEmpty(t, ctx.recordedHeartbeat, "%s: the marker stays", name)
		requireNoSentinel(t, result)
	}
}

func TestAddCommentAfterAnEarlierDispatchSendsNothing(t *testing.T) {
	provider := newRecordingTrello(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	first := newTrelloDexContext("comment-replay")
	first.recordedHeartbeat = json.RawMessage(`{"trelloDispatchedCallId":"earlier"}`)
	result, err := sdkgo.RunMutation(first.nextAttempt(), newTrelloClient(t, provider.URL).AddComment(), trelloConnection,
		trello.AddCommentInput{CardID: testCardID, Text: "Approved."})
	require.NoError(t, err)
	require.Equal(t, trello.AddCommentBranchUncertain, result.Branch)
	require.Zero(t, provider.requestCount())
}

func TestAddCommentRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingTrello(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	client := newTrelloClient(t, provider.URL)
	for name, input := range map[string]trello.AddCommentInput{
		"blank text":        {CardID: testCardID, Text: " \n "},
		"long text":         {CardID: testCardID, Text: strings.Repeat("x", 16385)},
		"control character": {CardID: testCardID, Text: "null\x00byte"},
		"invalid card":      {CardID: "card", Text: "Approved."},
	} {
		result, err := sdkgo.RunMutation(newTrelloDexContext("comment-invalid"), client.AddComment(), trelloConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, trello.AddCommentBranchDefect, result.Branch, name)
	}
}
