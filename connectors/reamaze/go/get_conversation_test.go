// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/reamaze"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetConversationReadsTheConversationAndItsNewestMessages(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if strings.HasSuffix(request.URL.Path, "/messages") {
			writeValue(t, response, http.StatusOK, map[string]any{
				"page_size": 30, "page_count": 2, "total_count": 31, "messages": []any{
					messageJSON("Internal: refund issued.", 1, "dex-abc"), messageJSON("We're on it.", 0, 1234567890), messageJSON("Hello", 0, nil),
				},
			})
			return
		}
		writeValue(t, response, http.StatusOK, conversationJSON("double-charge", 0, nil))
	})
	result, err := sdkgo.RunQuery(newReamazeDexContext("get"), newReamazeClient(t, provider.URL).GetConversation(), reamazeConnection,
		reamaze.GetConversationInput{ConversationID: "double-charge", MessageLimit: 2})
	require.NoError(t, err)
	require.Equal(t, reamaze.GetConversationBranchFound, result.Branch)
	require.Equal(t, "/api/v1/conversations/double-charge", provider.request(0).path)
	require.Equal(t, "/api/v1/conversations/double-charge/messages", provider.request(1).path)
	require.Equal(t, map[string][]string{"page": {"1"}}, provider.request(1).query)
	details := result.Value
	require.Equal(t, "I was charged twice.", details.Conversation.FirstMessage)
	require.NotNil(t, details.Conversation.LastStaffMessageAt)
	require.Len(t, details.Messages, 2, "messageLimit keeps the newest messages")
	require.True(t, details.HasMoreMessages)
	note := details.Messages[0]
	require.Equal(t, reamaze.MessageVisibilityInternalNote, note.Visibility)
	require.True(t, note.IsInternalNote)
	require.Equal(t, "dex-abc", note.OriginID)
	require.Equal(t, reamaze.MessageOrigin(7), note.Origin)
	require.Equal(t, &reamaze.ConversationParticipant{Name: "Agent", Email: testEmail}, note.Author)
	require.False(t, details.Messages[1].IsInternalNote)
	require.Equal(t, "1234567890", details.Messages[1].OriginID, "a numeric origin_id is kept as text")
	require.Equal(t, "double-charge", result.Receipt.ProviderObjectID)
}

func TestGetConversationTruncatesLongBodiesOnARuneBoundary(t *testing.T) {
	long := strings.Repeat("é", reamaze.MaxTextBytes)
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if strings.HasSuffix(request.URL.Path, "/messages") {
			writeValue(t, response, http.StatusOK, pageJSON("messages", []any{messageJSON(long, 0, nil)}, 1))
			return
		}
		conversation := conversationJSON("double-charge", 0, nil)
		conversation["message"] = map[string]any{"body": long}
		writeValue(t, response, http.StatusOK, conversation)
	})
	client, err := reamaze.New(reamaze.Config{Brand: testBrand, MaxResponseBytes: 1 << 20}, testCredentialProvider(), reamaze.WithAPIBaseURL(provider.URL+"/api/v1"))
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newReamazeDexContext("truncate"), client.GetConversation(), reamazeConnection, reamaze.GetConversationInput{ConversationID: "double-charge"})
	require.NoError(t, err)
	require.True(t, result.Value.Conversation.IsFirstMessageTruncated)
	require.LessOrEqual(t, len(result.Value.Conversation.FirstMessage), reamaze.MaxTextBytes)
	require.True(t, result.Value.Messages[0].IsBodyTruncated)
	require.False(t, result.Value.HasMoreMessages)
	require.True(t, strings.HasSuffix(result.Value.Messages[0].Body, "é"))
}

func TestGetConversationRejectsUnsafeSlugsAndForeignResponses(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, conversationJSON("another-conversation", 0, nil))
	})
	client := newReamazeClient(t, provider.URL)
	for _, slug := range []string{"", "../staff", "a/b", "a.json", "a?x=1", "-a", strings.Repeat("a", 300)} {
		result, err := sdkgo.RunQuery(newReamazeDexContext("unsafe"), client.GetConversation(), reamazeConnection, reamaze.GetConversationInput{ConversationID: slug})
		require.NoError(t, err)
		require.Equal(t, reamaze.GetConversationBranchDefect, result.Branch, "slug %q", slug)
	}
	result, err := sdkgo.RunQuery(newReamazeDexContext("limit"), client.GetConversation(), reamazeConnection,
		reamaze.GetConversationInput{ConversationID: "double-charge", MessageLimit: reamaze.MaxMessageLimit + 1})
	require.NoError(t, err)
	require.Equal(t, reamaze.GetConversationBranchDefect, result.Branch)
	require.Zero(t, provider.requestCount())

	result, err = sdkgo.RunQuery(newReamazeDexContext("foreign"), client.GetConversation(), reamazeConnection, reamaze.GetConversationInput{ConversationID: "double-charge"})
	require.NoError(t, err)
	require.Equal(t, reamaze.GetConversationBranchInvalidResponse, result.Branch, "a response for another slug is invalid")
}
