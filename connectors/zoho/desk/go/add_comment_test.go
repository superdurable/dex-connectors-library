// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func commentJSON(id string, isPublic bool, content string) map[string]any {
	return map[string]any{
		"id": id, "isPublic": isPublic, "contentType": "plainText", "content": content, "commenterId": testAgentID,
		"commentedTime": "2026-01-28T09:01:00.000Z", "modifiedTime": nil, "attachments": []any{},
		"commenter": map[string]any{"name": "Dex Integration", "type": "AGENT", "email": "SENTINEL@example.com"},
	}
}

func TestAddCommentSendsPlainTextWithTheRequestedVisibilityOnce(t *testing.T) {
	for _, isPublic := range []bool{false, true} {
		provider := newRecordingDesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
			var payload struct {
				Content  string `json:"content"`
				IsPublic bool   `json:"isPublic"`
			}
			require.NoError(t, json.Unmarshal(mustReadBody(t, request), &payload))
			writeValue(t, response, http.StatusOK, commentJSON("4000000529001", payload.IsPublic, payload.Content))
		})
		ctx := newDeskDexContext("comment")
		result, err := sdkgo.RunMutation(ctx, newDeskClient(t, provider.URL).AddComment(), deskConnection, desk.AddCommentInput{
			TicketID: testTicketID, Content: "Triaged: <b>refund</b> pending.\nPriority High.", IsPublic: isPublic,
		})
		require.NoError(t, err)
		require.Equal(t, desk.AddCommentBranchAdded, result.Branch)
		require.Equal(t, isPublic, result.Value.Comment.IsPublic)
		require.Equal(t, "Triaged: <b>refund</b> pending.\nPriority High.", result.Value.Comment.Content)
		require.Equal(t, "4000000529001", result.Receipt.ProviderObjectID)
		request := provider.request(0)
		require.Equal(t, "/api/v1/tickets/"+testTicketID+"/comments", request.path)
		expectedVisibility := "false"
		if isPublic {
			expectedVisibility = "true"
		}
		require.JSONEq(t, `{"content":"Triaged: <b>refund</b> pending.\nPriority High.","isPublic":`+expectedVisibility+`,"contentType":"plainText"}`, request.body,
			"a private comment states isPublic false instead of relying on Zoho Desk's default")
		require.NotEmpty(t, ctx.recordedHeartbeat)
		requireNoSentinel(t, result)
	}
}

func TestAddCommentNeverResendsAndRetriesOnlyAProvableNonApplication(t *testing.T) {
	for _, test := range []struct {
		name   string
		reply  func(http.ResponseWriter)
		branch sdkgo.BranchID
	}{
		{name: "lost response", reply: func(response http.ResponseWriter) { dropConnection(t, response) }, branch: desk.AddCommentBranchUncertain},
		{name: "server error", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusInternalServerError, `{"errorCode":"INTERNAL_SERVER_ERROR"}`)
		}, branch: desk.AddCommentBranchUncertain},
		{name: "visibility changed", reply: func(response http.ResponseWriter) {
			writeValue(t, response, http.StatusOK, commentJSON("4000000529001", true, "Triaged."))
		}, branch: desk.AddCommentBranchUncertain},
		{name: "missing ticket", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusNotFound, `{"errorCode":"URL_NOT_FOUND","message":"SENTINEL"}`)
		}, branch: desk.AddCommentBranchNotFound},
		{name: "too large", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusRequestEntityTooLarge, `{"errorCode":"RESOURCE_SIZE_EXCEEDED"}`)
		}, branch: desk.AddCommentBranchProviderRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) { test.reply(response) })
			result, err := sdkgo.RunMutation(newDeskDexContext("comment-"+test.name), newDeskClient(t, provider.URL).AddComment(), deskConnection,
				desk.AddCommentInput{TicketID: testTicketID, Content: "Triaged."})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, 1, provider.requestCount())
			requireNoSentinel(t, result)
		})
	}

	limited := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusTooManyRequests, `{"errorCode":"TOO_MANY_REQUESTS"}`)
	})
	ctx := newDeskDexContext("comment-limited")
	_, err := sdkgo.RunMutation(ctx, newDeskClient(t, limited.URL).AddComment(), deskConnection, desk.AddCommentInput{TicketID: testTicketID, Content: "Triaged."})
	requireRetry(t, err, sdkgo.FailureRateLimit)
	require.Nil(t, ctx.recordedHeartbeat, "a 429 clears the marker so the retry may send")
}

func TestAddCommentRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingDesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, input := range map[string]desk.AddCommentInput{
		"blank content":  {TicketID: testTicketID, Content: " \n"},
		"long content":   {TicketID: testTicketID, Content: strings.Repeat("c", desk.MaxCommentRunes+1)},
		"missing ticket": {Content: "Triaged."},
		"ticket number":  {TicketID: "101a", Content: "Triaged."},
	} {
		ctx := newDeskDexContext("comment-defect")
		result, err := sdkgo.RunMutation(ctx, newDeskClient(t, provider.URL).AddComment(), deskConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, desk.AddCommentBranchDefect, result.Branch, name)
		require.Zero(t, ctx.heartbeatCount, name)
	}
}
