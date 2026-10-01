// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom_test

import (
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intercom"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetConversationReturnsTheNewestPartsAsPlainText(t *testing.T) {
	longBody := strings.Repeat("é", intercom.MaxTextBytes)
	provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation, "snoozed", 1767312000,
			testPart{id: "101", partType: "comment", body: "Customer follow-up", authorType: "user", authorID: "5ba682d23d7cf92bef87bfd4", createdAt: 1767226000},
			testPart{id: "102", partType: "note", body: "Checking with billing", authorType: "admin", authorID: testAdminID, createdAt: 1767227000},
			testPart{id: "103", partType: "snoozed", body: "", authorType: "admin", authorID: testAdminID, createdAt: 1767228000},
			testPart{id: "104", partType: "comment", body: longBody, authorType: "user", authorID: "5ba682d23d7cf92bef87bfd4", createdAt: 1767228000},
		))
	})
	result, err := sdkgo.RunQuery(newTestDexContext("get"), newIntercomClient(t, provider.URL).GetConversation(), intercomConnection,
		intercom.GetConversationInput{ConversationID: testConversation, LatestPartLimit: 3})
	require.NoError(t, err)
	require.Equal(t, intercom.GetConversationBranchFound, result.Branch)
	details := result.Value
	require.Equal(t, "/conversations/"+testConversation+"?display_as=plaintext", provider.request(0).path)
	require.Equal(t, intercom.ConversationStateSnoozed, details.Conversation.State)
	require.NotNil(t, details.Conversation.SnoozedUntil)
	require.Equal(t, int64(1767312000), details.Conversation.SnoozedUntil.Unix())
	require.Equal(t, "I was charged twice for order 88213.", details.Conversation.Source.Body)
	require.Empty(t, details.Conversation.AdminAssigneeID, "API version 2.16 reports an unassigned admin as 0")
	require.Equal(t, "5017690", details.Conversation.TeamAssigneeID)
	require.Equal(t, "high", details.Conversation.Priority)
	require.Equal(t, []intercom.ConversationContact{{ID: "5ba682d23d7cf92bef87bfd4", ExternalID: "cont_010"}}, details.Conversation.Contacts)

	ids := []string{}
	for _, part := range details.LatestParts {
		ids = append(ids, part.ID)
	}
	require.Equal(t, []string{"104", "103", "102"}, ids, "newest first, ties broken by part ID")
	require.True(t, details.HasOlderParts)
	require.Equal(t, 4, details.PartCount)
	require.True(t, details.LatestParts[0].IsBodyTruncated)
	require.LessOrEqual(t, len(details.LatestParts[0].Body), intercom.MaxTextBytes)
	require.True(t, utf8.ValidString(details.LatestParts[0].Body))
	require.Equal(t, intercom.ConversationAuthor{Type: "admin", ID: testAdminID, Name: "Ada", Email: "ada@acme.example.com"}, details.LatestParts[2].Author)
}

func TestGetConversationValidatesInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingIntercom(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request may be sent") })
	client := newIntercomClient(t, provider.URL)
	for name, input := range map[string]intercom.GetConversationInput{
		"blank ID":       {},
		"path traversal": {ConversationID: "../admins"},
		"letters":        {ConversationID: "abc"},
		"part limit":     {ConversationID: testConversation, LatestPartLimit: intercom.MaxLatestPartLimit + 1},
		"negative limit": {ConversationID: testConversation, LatestPartLimit: -1},
	} {
		result, err := sdkgo.RunQuery(newTestDexContext("get-"+name), client.GetConversation(), intercomConnection, input)
		require.NoError(t, err)
		require.Equal(t, intercom.GetConversationBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
	}
}

// TestConversationDecodingToleratesEarlierAPIVersionShapes covers webhook payloads in an app's older API version.
func TestConversationDecodingToleratesEarlierAPIVersionShapes(t *testing.T) {
	provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"type":"conversation","id":`+testConversation+`,"state":"open","open":true,
			"created_at":"1767225600","updated_at":1767229200,"admin_assignee_id":null,"team_assignee_id":"991268013","priority":"priority",
			"source":{"author":{"type":"lead","id":12345,"email":"jane@acme.example.com"}},"conversation_parts":{"conversation_parts":[]}}`)
	})
	result, err := sdkgo.RunQuery(newTestDexContext("get-older"), newIntercomClient(t, provider.URL).GetConversation(), intercomConnection,
		intercom.GetConversationInput{ConversationID: testConversation})
	require.NoError(t, err)
	require.Equal(t, intercom.GetConversationBranchFound, result.Branch)
	require.Equal(t, testConversation, result.Value.Conversation.ID)
	require.Empty(t, result.Value.Conversation.AdminAssigneeID)
	require.Equal(t, "991268013", result.Value.Conversation.TeamAssigneeID)
	require.Equal(t, "priority", result.Value.Conversation.Priority, "Intercom's own values pass through")
	require.Equal(t, "12345", result.Value.Conversation.Source.Author.ID)
	require.Equal(t, int64(1767225600), result.Value.Conversation.CreatedAt.Unix())
	require.Empty(t, result.Value.LatestParts)
	require.False(t, result.Value.HasOlderParts)
}
