// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front_test

import (
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/front"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetConversationReadsTheNewestMessagesAndComments(t *testing.T) {
	longBody := strings.Repeat("é", front.MaxBodyBytes)
	provider := newRecordingFront(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		switch request.URL.Path {
		case "/conversations/" + testConversationID:
			writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation{id: testConversationID, status: "assigned", statusID: "sts_5x",
				assigneeID: testTeammateID, tagIDs: []string{testTagID}}))
		case "/conversations/" + testConversationID + "/messages":
			writeJSON(t, response, http.StatusOK, `{"_pagination":{"next":"https://acme.api.frontapp.com/conversations/`+testConversationID+`/messages?limit=2&page_token=abc123"},
				"_results":[
				{"id":"msg_2","type":"email","is_inbound":false,"draft_mode":null,"subject":"Re: Double charge","blurb":"We are on it","body":"<p>We are on it</p>",
				 "author":{"id":"`+testTeammateID+`","email":"leela@planet-express.example.com"},"recipients":[{"handle":"jane@acme.example.com","role":"to"}],"created_at":1767228000},
				{"id":"msg_1","type":"email","is_inbound":true,"blurb":"Charged twice","body":"`+longBody+`","author":null,
				 "recipients":[{"_links":{"related":{"contact":"https://acme.api.frontapp.com/contacts/crd_1y8sp71"}},"handle":"jane@acme.example.com","role":"from","name":"Jane"}],"created_at":1767226000.5}]}`)
		case "/conversations/" + testConversationID + "/comments":
			writeJSON(t, response, http.StatusOK, `{"_results":[
				{"id":"com_3","body":"Refund already issued","author":{"id":"`+testTeammateID+`"},"is_pinned":true,"posted_at":1767228500},
				{"id":"com_2","body":"Checking billing","author":{"id":"`+testTeammateID+`"},"is_pinned":false,"posted_at":1767227500},
				{"id":"com_1","body":"Old note","author":null,"posted_at":1767226500}]}`)
		default:
			t.Errorf("unexpected request %s", request.URL.Path)
		}
	})
	result, err := sdkgo.RunQuery(newTestDexContext("get"), newFrontClient(t, provider.URL).GetConversation(), frontConnection,
		front.GetConversationInput{ConversationID: testConversationID, MessageLimit: 2, CommentLimit: 2})
	require.NoError(t, err)
	require.Equal(t, front.GetConversationBranchFound, result.Branch)
	require.Equal(t, "/conversations/"+testConversationID+"/messages?limit=2", provider.request(1).path)
	details := result.Value
	require.Equal(t, front.ConversationStatusAssigned, details.Conversation.Status)
	require.Equal(t, "sts_5x", details.Conversation.StatusID)
	require.Equal(t, front.ConversationStatusCategoryOpen, details.Conversation.StatusCategory)
	require.Equal(t, []string{"TICKET-1"}, details.Conversation.TicketIDs)
	require.NotNil(t, details.Conversation.WaitingSince)
	require.Len(t, details.Messages, 2)
	require.True(t, details.HasOlderMessages)
	reply, inbound := details.Messages[0], details.Messages[1]
	require.Equal(t, "msg_2", reply.ID)
	require.False(t, reply.IsInbound)
	require.Equal(t, testTeammateID, reply.Author.ID)
	require.True(t, inbound.IsInbound)
	require.Nil(t, inbound.Author)
	require.Equal(t, []front.Recipient{{Handle: "jane@acme.example.com", Role: "from", Name: "Jane", ContactID: "crd_1y8sp71"}}, inbound.Recipients)
	require.True(t, inbound.IsBodyTruncated)
	require.LessOrEqual(t, len(inbound.Body), front.MaxBodyBytes)
	require.True(t, utf8.ValidString(inbound.Body))
	require.Equal(t, []string{"com_3", "com_2"}, []string{details.Comments[0].ID, details.Comments[1].ID})
	require.True(t, details.HasOlderComments)
	require.True(t, details.Comments[0].IsPinned)
}

func TestGetConversationSelectsMergedWithoutFollowingTheRedirect(t *testing.T) {
	provider := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.Header().Set("Location", "https://api2.frontapp.com/conversations/cnv_yo1kg5q")
		writeFrontError(t, response, http.StatusMovedPermanently)
	})
	result, err := sdkgo.RunQuery(newTestDexContext("get-merged"), newFrontClient(t, provider.URL).GetConversation(), frontConnection,
		front.GetConversationInput{ConversationID: testConversationID})
	require.NoError(t, err)
	require.Equal(t, front.GetConversationBranchMerged, result.Branch)
	require.Equal(t, "cnv_yo1kg5q", result.Value.MergedIntoConversationID)
	require.Equal(t, 1, provider.requestCount(), "the redirect is never followed")
}

func TestGetConversationMapsMissingAndInvalidAnswers(t *testing.T) {
	for name, test := range map[string]struct {
		respond func(response http.ResponseWriter)
		branch  sdkgo.BranchID
	}{
		"missing":   {func(response http.ResponseWriter) { writeFrontError(t, response, http.StatusNotFound) }, front.GetConversationBranchNotFound},
		"forbidden": {func(response http.ResponseWriter) { writeFrontError(t, response, http.StatusForbidden) }, front.GetConversationBranchProviderRejected},
		"malformed": {func(response http.ResponseWriter) { writeJSON(t, response, http.StatusOK, `{"id":`) }, front.GetConversationBranchInvalidResponse},
		"another ID": {func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation{id: "cnv_other", status: "assigned"}))
		}, front.GetConversationBranchInvalidResponse},
		"reflected token":   {func(response http.ResponseWriter) { writeJSON(t, response, http.StatusOK, `{"id":"`+testAPIToken+`"}`) }, front.GetConversationBranchInvalidResponse},
		"other redirection": {func(response http.ResponseWriter) { writeFrontError(t, response, http.StatusFound) }, front.GetConversationBranchProviderRejected},
	} {
		provider := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, _ int) { test.respond(response) })
		result, err := sdkgo.RunQuery(newTestDexContext("get-"+name), newFrontClient(t, provider.URL).GetConversation(), frontConnection,
			front.GetConversationInput{ConversationID: testConversationID})
		require.NoError(t, err, name)
		require.Equal(t, test.branch, result.Branch, name)
		require.NotContains(t, result.Failure.Message, providerSentinel, name)
		require.NotContains(t, result.Failure.Message, testAPIToken, name)
	}
}

func TestGetConversationRetriesAServerError(t *testing.T) {
	provider := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeFrontError(t, response, http.StatusBadGateway)
	})
	_, err := sdkgo.RunQuery(newTestDexContext("get-502"), newFrontClient(t, provider.URL).GetConversation(), frontConnection,
		front.GetConversationInput{ConversationID: testConversationID})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
}

func TestGetConversationValidatesInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingFront(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request may be sent") })
	client := newFrontClient(t, provider.URL)
	for name, input := range map[string]front.GetConversationInput{
		"blank ID":       {},
		"path traversal": {ConversationID: "../contacts"},
		"ticket number":  {ConversationID: "TICKET-1"},
		"message limit":  {ConversationID: testConversationID, MessageLimit: front.MaxMessageLimit + 1},
		"comment limit":  {ConversationID: testConversationID, CommentLimit: -1},
	} {
		result, err := sdkgo.RunQuery(newTestDexContext("get-"+name), client.GetConversation(), frontConnection, input)
		require.NoError(t, err)
		require.Equal(t, front.GetConversationBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind, name)
	}
}
